package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/sandbox"
)

// sandboxForm renders a small sandbox in the project namespace through the
// same path the GUI uses.
func sandboxForm(name string) *unstructured.Unstructured {
	f := &sandbox.Form{Name: name, Image: "busybox", Command: "sleep 1d", Service: true}
	pcfg, err := platform.Parse([]byte(testPlatformConfig))
	Expect(err).NotTo(HaveOccurred())
	u, err := sandbox.Render(f, sandbox.Context{Namespace: sandboxNS, Project: sandboxProject, Platform: pcfg})
	Expect(err).NotTo(HaveOccurred())
	return u
}

// setSandboxStatus marks a sandbox as Ready with the backing pod running.
func setSandboxStatus(name, pod string) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(sandbox.SandboxGVK)
	Expect(get(u, sandboxNS, name)()).To(Succeed())
	Expect(unstructured.SetNestedSlice(u.Object, []any{map[string]any{
		"type": "Ready", "status": "True", "reason": "DependenciesReady", "message": "sandbox is ready",
		"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
	}}, "status", "conditions")).To(Succeed())
	Expect(unstructured.SetNestedField(u.Object, pod, "status", "serviceFQDN")).To(Succeed())
	Expect(k8sClient.Status().Update(ctx, u)).To(Succeed())
}

func getSandbox(name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(sandbox.SandboxGVK)
	ExpectWithOffset(1, get(u, sandboxNS, name)()).To(Succeed())
	return u
}

const (
	sandboxProject = "agents"
	sandboxNS      = "forge-" + sandboxProject
)

var _ = Describe("Sandbox controller", Ordered, func() {
	BeforeAll(func() {
		p := &v1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: sandboxProject},
			Spec: v1.ProjectSpec{
				Quota:   v1.ProjectQuota{GPUs: []v1.GPUQuota{{Class: "rtx-5090", Count: 1}}},
				Network: v1.ProjectNetwork{Isolation: v1.NetworkIsolated},
			},
		}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())
		Eventually(func() bool {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(p), p)
			return meta.IsStatusConditionTrue(p.Status.Conditions, v1.ConditionReady)
		}, timeout, interval).Should(BeTrue())
	})

	It("stops and starts a sandbox from the stop annotation", func() {
		Expect(k8sClient.Create(ctx, sandboxForm("agent"))).To(Succeed())
		setSandboxStatus("agent", "agent")

		// The controller writes the idle heartbeat, so annotate with a retry.
		Eventually(func() error {
			u := getSandbox("agent")
			Expect(unstructured.SetNestedMap(u.Object, map[string]any{naming.AnnotationStopped: "true"},
				"metadata", "annotations")).To(Succeed())
			return k8sClient.Update(ctx, u)
		}, timeout, interval).Should(Succeed())

		Eventually(func() string {
			mode, _, _ := unstructured.NestedString(getSandbox("agent").Object, "spec", "operatingMode")
			return mode
		}, timeout, interval).Should(Equal(string(sandbox.OperatingSuspended)))

		// Removing the annotation starts it again and resets the idle clock.
		Eventually(func() error {
			u := getSandbox("agent")
			unstructured.RemoveNestedField(u.Object, "metadata", "annotations", naming.AnnotationStopped)
			return k8sClient.Update(ctx, u)
		}, timeout, interval).Should(Succeed())
		Eventually(func() string {
			mode, _, _ := unstructured.NestedString(getSandbox("agent").Object, "spec", "operatingMode")
			return mode
		}, timeout, interval).Should(Equal(string(sandbox.OperatingRunning)))
		Expect(getSandbox("agent").GetAnnotations()).To(HaveKey(naming.AnnotationLastActive))
	})

	It("gives a sandbox with a heartbeat one idle timeout to live", func() {
		Expect(k8sClient.Create(ctx, sandboxForm("idle"))).To(Succeed())
		setSandboxStatus("idle", "idle")
		pod := &sandboxPod{name: "idle"}
		Expect(pod.create()).To(Succeed())

		// A Ready sandbox without a heartbeat gets one from the controller.
		Eventually(func() string {
			return getSandbox("idle").GetAnnotations()[naming.AnnotationLastActive]
		}, timeout, interval).ShouldNot(BeEmpty())

		// Backdate the heartbeat past the 1h timeout of the test config: the
		// next reconcile (triggered by the pod) suspends the sandbox.
		old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
		Eventually(func() error {
			u := getSandbox("idle")
			Expect(unstructured.SetNestedField(u.Object, old,
				"metadata", "annotations", naming.AnnotationLastActive)).To(Succeed())
			return k8sClient.Update(ctx, u)
		}, timeout, interval).Should(Succeed())

		Eventually(func() map[string]string {
			return getSandbox("idle").GetAnnotations()
		}, timeout, interval).Should(And(
			HaveKeyWithValue(naming.AnnotationStopped, "true"),
			HaveKey(naming.AnnotationIdleSince),
		))
	})
})

// sandboxPod creates the backing pod of a sandbox, owned by it.
type sandboxPod struct{ name string }

func (p *sandboxPod) create() error {
	sb := getSandbox(p.name)
	sb.SetGroupVersionKind(sandbox.SandboxGVK)
	labels := map[string]any{
		naming.LabelProject:      sandboxProject,
		sandbox.LabelSandboxName: p.name,
	}
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{
			"name": p.name, "namespace": sandboxNS, "labels": labels,
			"ownerReferences": []any{map[string]any{
				"apiVersion": sandbox.SandboxGVK.GroupVersion().String(), "kind": "Sandbox",
				"name": p.name, "uid": string(sb.GetUID()), "controller": true, "blockOwnerDeletion": true,
			}},
		},
		"spec": map[string]any{"containers": []any{map[string]any{"name": "sandbox", "image": "busybox"}}},
	}}
	pod := &unstructured.Unstructured{Object: u.Object}
	return client.IgnoreAlreadyExists(k8sClient.Create(ctx, pod))
}

package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/sandbox"
	"github.com/wutz/forgeai/pkg/sshgateway"
)

// sandboxForm renders a small sandbox in the project namespace through the
// same path the GUI uses.
func sandboxForm(name string) *unstructured.Unstructured {
	return renderSandbox(&sandbox.Form{Name: name, Image: "busybox", Command: "sleep 1d", Service: true})
}

func renderSandbox(f *sandbox.Form) *unstructured.Unstructured {
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
		// The project operator creates this namespace from a Project; the sandbox
		// controller only reads its label.
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: sandboxNS, Labels: map[string]string{naming.LabelProject: sandboxProject},
		}})).To(Succeed())
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
		startIdleSandbox("idle")
		waitForHeartbeat("idle")

		// Backdate the heartbeat past the 1h timeout of the test config: the
		// next reconcile (triggered by the pod) suspends the sandbox.
		backdateHeartbeat("idle", 2*time.Hour)

		Eventually(func() map[string]string {
			return getSandbox("idle").GetAnnotations()
		}, timeout, interval).Should(And(
			HaveKeyWithValue(naming.AnnotationStopped, "true"),
			HaveKey(naming.AnnotationIdleSince),
		))
	})

	It("gives an SSH sandbox a Service and a host key Secret that trust only the platform key", func() {
		Expect(k8sClient.Create(ctx, renderSandbox(&sandbox.Form{
			Name: "ssh-agent", Image: "busybox", Command: "sleep 1d", SSH: true,
		}))).To(Succeed())

		svc := &corev1.Service{}
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{Namespace: sandboxNS, Name: naming.SSHServiceName("ssh-agent")}, svc)
		}, timeout, interval).Should(Succeed())
		Expect(svc.Spec.Selector).To(Equal(map[string]string{naming.LabelSandbox: "ssh-agent"}))
		Expect(svc.Spec.Ports).To(HaveLen(1))
		Expect(svc.Spec.Ports[0].Port).To(BeEquivalentTo(sandbox.SSHPort))
		Expect(svc.OwnerReferences).To(HaveLen(1))
		Expect(svc.OwnerReferences[0].Kind).To(Equal(sandbox.SandboxGVK.Kind))

		sec := &corev1.Secret{}
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{
				Namespace: sandboxNS, Name: naming.SSHHostKeySecret(naming.SSHServiceName("ssh-agent")),
			}, sec)
		}, timeout, interval).Should(Succeed())
		Expect(sec.Data).To(HaveKey(sshgateway.SSHHostKeyKey))
		Expect(sec.OwnerReferences).To(HaveLen(1))

		identity := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Namespace: configNamespace, Name: naming.SSHIdentitySecretName,
		}, identity)).To(Succeed())
		Expect(identity.Data[sshPrivateKeyKey]).NotTo(BeEmpty())
		// The sandbox accepts the platform public key and no other key.
		Expect(sec.Data[sshgateway.SSHAuthorizedKeyKey]).To(Equal(identity.Data[sshPublicKeyKey]))
	})

	It("uses the idle timeout a sandbox was created with", func() {
		// A 1m timeout suspends it after 2m without a heartbeat, well inside
		// the 1h platform default.
		Expect(k8sClient.Create(ctx, renderSandbox(&sandbox.Form{
			Name: "short", Image: "busybox", Command: "sleep 1d", Service: true,
			IdleTimeout: &metav1.Duration{Duration: time.Minute},
		}))).To(Succeed())
		startIdleSandbox("short")
		waitForHeartbeat("short")
		backdateHeartbeat("short", 2*time.Minute)

		Eventually(func() string {
			return getSandbox("short").GetAnnotations()[naming.AnnotationStopped]
		}, timeout, interval).Should(Equal("true"))
	})

	It("keeps a sandbox created with idle timeout 0 running", func() {
		// 0 turns the idle policy off for this sandbox, even though the
		// platform default is 1h and the heartbeat is two hours old.
		Expect(k8sClient.Create(ctx, renderSandbox(&sandbox.Form{
			Name: "forever", Image: "busybox", Command: "sleep 1d", Service: true,
			IdleTimeout: &metav1.Duration{},
		}))).To(Succeed())
		startIdleSandbox("forever")
		backdateHeartbeat("forever", 2*time.Hour)

		Consistently(func() string {
			return getSandbox("forever").GetAnnotations()[naming.AnnotationStopped]
		}, 2*time.Second, interval).ShouldNot(Equal("true"))
	})

	It("publishes a sandbox port through the gateway", func() {
		Expect(k8sClient.Create(ctx, renderSandbox(&sandbox.Form{
			Name: "web", Image: "busybox", Command: "sleep 1d", Service: true, GatewayPort: 8080,
		}))).To(Succeed())
		setSandboxStatus("web", "web")
		Expect((&sandboxPod{name: "web"}).create()).To(Succeed())

		svc := &corev1.Service{}
		Eventually(get(svc, sandboxNS, "web-gw"), timeout, interval).Should(Succeed())
		Expect(svc.Spec.Selector).To(HaveKeyWithValue(sandbox.LabelSandboxNameHash, sandboxPodHash("web")))
		Expect(svc.Spec.Ports).To(HaveLen(1))
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8080)))
		Expect(metav1.IsControlledBy(svc, getSandbox("web"))).To(BeTrue())

		route := &unstructured.Unstructured{}
		route.SetGroupVersionKind(sandbox.HTTPRouteGVK)
		Eventually(get(route, sandboxNS, "web"), timeout, interval).Should(Succeed())
		hosts, _, _ := unstructured.NestedStringSlice(route.Object, "spec", "hostnames")
		Expect(hosts).To(Equal([]string{"web-forge-agents.example.com"}))
		parents, _, _ := unstructured.NestedSlice(route.Object, "spec", "parentRefs")
		Expect(parents).To(HaveLen(1))
		backends, _, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
		Expect(backends).To(HaveLen(1))
		Expect(route.GetOwnerReferences()).To(HaveLen(1))
	})

	It("removes the gateway objects when the port is cleared", func() {
		Eventually(func() error {
			u := getSandbox("web")
			unstructured.RemoveNestedField(u.Object, "metadata", "annotations", naming.AnnotationGatewayPort)
			return k8sClient.Update(ctx, u)
		}, timeout, interval).Should(Succeed())

		Eventually(func() bool {
			return apierrors.IsNotFound(get(&corev1.Service{}, sandboxNS, "web-gw")())
		}, timeout, interval).Should(BeTrue())
		route := &unstructured.Unstructured{}
		route.SetGroupVersionKind(sandbox.HTTPRouteGVK)
		Eventually(func() bool {
			return apierrors.IsNotFound(get(route, sandboxNS, "web")())
		}, timeout, interval).Should(BeTrue())
	})

	It("refuses to take over a gateway Service it does not own", func() {
		Expect(k8sClient.Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "taken-gw", Namespace: sandboxNS, Labels: map[string]string{
				naming.LabelManagedBy: naming.ManagedBy,
			}},
			Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 80}}},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, renderSandbox(&sandbox.Form{
			Name: "taken", Image: "busybox", Command: "sleep 1d", Service: true, GatewayPort: 8080,
		}))).To(Succeed())
		setSandboxStatus("taken", "taken")
		Expect((&sandboxPod{name: "taken"}).create()).To(Succeed())

		route := &unstructured.Unstructured{}
		route.SetGroupVersionKind(sandbox.HTTPRouteGVK)
		Consistently(func() bool {
			return apierrors.IsNotFound(get(route, sandboxNS, "taken")())
		}, 2*time.Second, interval).Should(BeTrue())
		svc := &corev1.Service{}
		Expect(get(svc, sandboxNS, "taken-gw")()).To(Succeed())
		Expect(svc.OwnerReferences).To(BeEmpty())
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(80)))
	})
})

// sandboxPodHash is the name hash the agent-sandbox controller labels the pods
// of a sandbox with.
func sandboxPodHash(name string) string { return "hash-" + name }

// startIdleSandbox marks a sandbox Ready with its pod running.
func startIdleSandbox(name string) {
	setSandboxStatus(name, name)
	pod := &sandboxPod{name: name}
	Expect(pod.create()).To(Succeed())
}

// waitForHeartbeat waits for the controller to start a Ready sandbox's idle clock.
func waitForHeartbeat(name string) {
	Eventually(func() string {
		return getSandbox(name).GetAnnotations()[naming.AnnotationLastActive]
	}, timeout, interval).ShouldNot(BeEmpty())
}

// backdateHeartbeat moves a sandbox's heartbeat back in time.
func backdateHeartbeat(name string, ago time.Duration) {
	old := time.Now().Add(-ago).UTC().Format(time.RFC3339)
	Eventually(func() error {
		u := getSandbox(name)
		Expect(unstructured.SetNestedField(u.Object, old,
			"metadata", "annotations", naming.AnnotationLastActive)).To(Succeed())
		return k8sClient.Update(ctx, u)
	}, timeout, interval).Should(Succeed())
}

// sandboxPod creates the backing pod of a sandbox, owned by it.
type sandboxPod struct{ name string }

func (p *sandboxPod) create() error {
	sb := getSandbox(p.name)
	sb.SetGroupVersionKind(sandbox.SandboxGVK)
	labels := map[string]any{
		naming.LabelProject:          sandboxProject,
		sandbox.LabelSandboxName:     p.name,
		sandbox.LabelSandboxNameHash: sandboxPodHash(p.name),
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

package controller

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	instancetypev1 "github.com/wutz/forgeai-instancetype-operator/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/training"
	"github.com/wutz/forgeai/pkg/volumes"
)

const (
	timeout    = 20 * time.Second
	interval   = 200 * time.Millisecond
	gpuProduct = "NVIDIA-GeForce-RTX-5090"
)

func createGPUNode(name string) {
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
		naming.LabelGPUProduct: gpuProduct, naming.LabelGPUMemory: "32607",
	}}}
	Expect(k8sClient.Create(ctx, n)).To(Succeed())
	n.Status = corev1.NodeStatus{
		Capacity: corev1.ResourceList{"cpu": resource.MustParse("128"), "memory": resource.MustParse("1Ti"),
			"nvidia.com/gpu": resource.MustParse("8"), "pods": resource.MustParse("110")},
		Allocatable: corev1.ResourceList{"cpu": resource.MustParse("128"), "memory": resource.MustParse("1Ti"),
			"nvidia.com/gpu": resource.MustParse("8"), "pods": resource.MustParse("110")},
		Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue,
			LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "KubeletReady"}},
	}
	Expect(k8sClient.Status().Update(ctx, n)).To(Succeed())
	// The API server taints new nodes not-ready; without a node lifecycle
	// controller in envtest the taint stays, so drop it.
	Eventually(func() error {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(n), n); err != nil {
			return err
		}
		n.Spec.Taints = nil
		return k8sClient.Update(ctx, n)
	}, timeout, interval).Should(Succeed())
}

func get(obj client.Object, ns, name string) func() error {
	return func() error { return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, obj) }
}

func trainJob(ns, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(kf.TrainJobGVK)
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, u)).To(Succeed())
	return u
}

// failTrainJob marks a TrainJob Failed as the Trainer does. The operator
// annotates the same object while it runs, so a copy read earlier can conflict;
// the update is retried on a fresh copy.
func failTrainJob(ns, name string) {
	Eventually(func() error {
		u := trainJob(ns, name)
		if err := unstructured.SetNestedSlice(u.Object, []any{map[string]any{
			"type": "Failed", "status": "True", "reason": "FailedJobs", "message": "jobset failed",
			"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
		}}, "status", "conditions"); err != nil {
			return err
		}
		return k8sClient.Status().Update(ctx, u)
	}, timeout, interval).Should(Succeed())
}

var _ = Describe("forgeai operator", Ordered, func() {
	const project = "demo"
	ns := "forge-" + project

	BeforeAll(func() {
		createGPUNode("gn-1")
		createGPUNode("gn-2")
		// The project operator creates this namespace from a Project; the
		// remaining cases only need its name and label.
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name: ns, Labels: map[string]string{naming.LabelProject: project},
		}})).To(Succeed())
	})

	It("derives instance types and resource flavors from GPU nodes", func() {
		it := &instancetypev1.InstanceType{}
		Eventually(get(it, "", "rtx-5090.x2"), timeout, interval).Should(Succeed())
		// (128 - 8) / 8 = 15 cores, (1024 - 64) / 8 = 120 GiB per GPU.
		Expect(it.Spec.CPU.String()).To(Equal("30"))
		Expect(it.Spec.Memory.String()).To(Equal("240Gi"))
		Expect(it.Spec.NodeSelector).To(HaveKeyWithValue(naming.LabelGPUProduct, gpuProduct))
		Eventually(func() int32 {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(it), it)
			return it.Status.Total
		}, timeout, interval).Should(Equal(int32(8)), func() string {
			n := &corev1.Node{}
			_ = get(n, "", "gn-1")()
			return fmt.Sprintf("%+v\n%+v\n%+v", it.Status, n.Status.Conditions, n.Spec.Taints)
		})
		Expect(it.Status.Free).To(Equal(int32(8)))

		rf := &unstructured.Unstructured{}
		rf.SetGroupVersionKind(kf.ResourceFlavorGVK)
		Eventually(get(rf, "", "forgeai-rtx-5090"), timeout, interval).Should(Succeed())

		// Manually deleted instance types come back.
		Expect(k8sClient.Delete(ctx, it)).To(Succeed())
		Eventually(func() bool {
			cur := &instancetypev1.InstanceType{}
			return k8sClient.Get(ctx, client.ObjectKeyFromObject(it), cur) == nil && cur.UID != it.UID
		}, timeout, interval).Should(BeTrue())
	})

	It("purges expired volumes from the trash", func() {
		pcfg, err := platform.Parse([]byte(testPlatformConfig))
		Expect(err).NotTo(HaveOccurred())
		vc := &volumes.Client{K8s: k8sClient, Namespace: ns}
		_, err = vc.Create(ctx, pcfg, "keep", "", 10)
		Expect(err).NotTo(HaveOccurred())
		_, err = vc.Create(ctx, pcfg, "scratch", "", 10)
		Expect(err).NotTo(HaveOccurred())
		_, err = vc.Trash(ctx, "keep", time.Hour)
		Expect(err).NotTo(HaveOccurred())
		_, err = vc.Trash(ctx, "scratch", time.Second)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() bool {
			pvc := &corev1.PersistentVolumeClaim{}
			err := get(pvc, ns, "scratch")()
			// envtest has no PVC protection controller, but kube-controller-manager
			// would add a finalizer; either way deletion has been requested.
			return apierrors.IsNotFound(err) || (err == nil && !pvc.DeletionTimestamp.IsZero())
		}, timeout, interval).Should(BeTrue())
		Consistently(get(&corev1.PersistentVolumeClaim{}, ns, "keep"), 2*time.Second, interval).Should(Succeed())
		_, err = vc.Restore(ctx, "keep")
		Expect(err).NotTo(HaveOccurred())
	})

	It("classifies a failed TrainJob and retries infrastructure failures", func() {
		f := &training.Form{Name: "train", Image: "img", InstanceType: "rtx-5090.x1", Workers: 1, MaxRetries: 1}
		it := &instancetypev1.InstanceType{}
		Expect(get(it, "", "rtx-5090.x1")()).To(Succeed())
		pcfg, _ := platform.Parse([]byte(testPlatformConfig))
		tj, err := training.TrainJob(f, training.Context{Namespace: ns, InstanceType: it, Config: pcfg})
		Expect(err).NotTo(HaveOccurred())
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tj)
		Expect(err).NotTo(HaveOccurred())
		u := &unstructured.Unstructured{Object: m}
		unstructured.RemoveNestedField(u.Object, "metadata", "creationTimestamp")
		Expect(k8sClient.Create(ctx, u)).To(Succeed())

		// A worker pod killed by node failure, then the TrainJob fails.
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "train-node-0-0-x", Namespace: ns, Labels: map[string]string{
				kf.LabelJobSetName: "train", kf.LabelCompletion: "0",
			}},
			Spec: corev1.PodSpec{NodeName: "gn-gone", Containers: []corev1.Container{{Name: "node", Image: "img"}}},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		pod.Status = corev1.PodStatus{Phase: corev1.PodFailed, ContainerStatuses: []corev1.ContainerStatus{{
			Name: "node", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 137, Reason: "Error"}},
		}}}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

		failTrainJob(ns, "train")

		Eventually(func() map[string]string {
			return trainJob(ns, "train").GetAnnotations()
		}, timeout, interval).Should(And(
			HaveKeyWithValue(naming.AnnotationFailureReason, training.ReasonNodeFailure),
			HaveKeyWithValue(naming.AnnotationRetriedAs, "train-r1"),
		))
		retry := trainJob(ns, "train-r1")
		Expect(retry.GetLabels()).To(HaveKeyWithValue(naming.LabelJobGroup, "train"))
		Expect(retry.GetAnnotations()).To(HaveKeyWithValue(naming.AnnotationRetry, "1"))
		patches, _, _ := unstructured.NestedSlice(retry.Object, "spec", "runtimePatches")
		Expect(patches).To(HaveLen(1))
	})

	It("fails jobs stuck on image pulls without retrying them", func() {
		f := &training.Form{Name: "stuck", Image: "nope", InstanceType: "rtx-5090.x1", Workers: 1, MaxRetries: 3, RetryOn: training.RetryAny}
		it := &instancetypev1.InstanceType{}
		Expect(get(it, "", "rtx-5090.x1")()).To(Succeed())
		pcfg, _ := platform.Parse([]byte(testPlatformConfig))
		tj, _ := training.TrainJob(f, training.Context{Namespace: ns, InstanceType: it, Config: pcfg})
		m, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(tj)
		u := &unstructured.Unstructured{Object: m}
		unstructured.RemoveNestedField(u.Object, "metadata", "creationTimestamp")
		Expect(k8sClient.Create(ctx, u)).To(Succeed())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "stuck-node-0-0-x", Namespace: ns, Labels: map[string]string{
				kf.LabelJobSetName: "stuck", kf.LabelCompletion: "0",
			}},
			Spec: corev1.PodSpec{NodeName: "gn-1", Containers: []corev1.Container{{Name: "node", Image: "nope"}}},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		pod.Status = corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{
			Name: "node", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "not found"}},
		}}}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

		Eventually(func() map[string]string {
			return trainJob(ns, "stuck").GetAnnotations()
		}, timeout, interval).Should(And(
			HaveKeyWithValue(naming.AnnotationFailureReason, training.ReasonImagePullFailed),
			HaveKeyWithValue(training.AnnotationStopped, "true"),
		))
		Consistently(func() bool {
			return apierrors.IsNotFound(get(&unstructured.Unstructured{Object: map[string]any{
				"apiVersion": kf.TrainJobGVK.GroupVersion().String(), "kind": "TrainJob"}}, ns, "stuck-r1")())
		}, 2*time.Second, interval).Should(BeTrue())
	})

	It("keeps the worker failure seen while a TrainJob ran once its pods are gone", func() {
		f := &training.Form{Name: "gone", Image: "img", InstanceType: "rtx-5090.x1", Workers: 1}
		it := &instancetypev1.InstanceType{}
		Expect(get(it, "", "rtx-5090.x1")()).To(Succeed())
		pcfg, _ := platform.Parse([]byte(testPlatformConfig))
		tj, err := training.TrainJob(f, training.Context{Namespace: ns, InstanceType: it, Config: pcfg})
		Expect(err).NotTo(HaveOccurred())
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tj)
		Expect(err).NotTo(HaveOccurred())
		u := &unstructured.Unstructured{Object: m}
		unstructured.RemoveNestedField(u.Object, "metadata", "creationTimestamp")
		Expect(k8sClient.Create(ctx, u)).To(Succeed())

		// The worker runs out of memory while the job runs.
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "gone-node-0-0-x", Namespace: ns, Labels: map[string]string{
				kf.LabelJobSetName: "gone", kf.LabelCompletion: "0",
			}},
			Spec: corev1.PodSpec{NodeName: "gn-1", Containers: []corev1.Container{{Name: "node", Image: "img"}}},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		pod.Status = corev1.PodStatus{Phase: corev1.PodFailed, ContainerStatuses: []corev1.ContainerStatus{{
			Name: "node", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 137, Reason: "OOMKilled"}},
		}}}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
		Eventually(func() map[string]string {
			return trainJob(ns, "gone").GetAnnotations()
		}, timeout, interval).Should(HaveKeyWithValue(naming.AnnotationObservedFailureReason, training.ReasonOOMKilled))

		// The Trainer removes the pod when it fails the job; nothing is left to classify.
		Expect(k8sClient.Delete(ctx, pod, client.GracePeriodSeconds(0))).To(Succeed())
		failTrainJob(ns, "gone")

		Eventually(func() map[string]string {
			return trainJob(ns, "gone").GetAnnotations()
		}, timeout, interval).Should(And(
			HaveKeyWithValue(naming.AnnotationFailureReason, training.ReasonOOMKilled),
			HaveKeyWithValue(naming.AnnotationFailureMessage, "worker-0 ran out of memory"),
		))
	})

	It("deactivates the Kueue Workload of a stopped TrainJob and reactivates it on resume", func() {
		f := &training.Form{Name: "pause", Image: "img", InstanceType: "rtx-5090.x1", Workers: 1}
		it := &instancetypev1.InstanceType{}
		Expect(get(it, "", "rtx-5090.x1")()).To(Succeed())
		pcfg, _ := platform.Parse([]byte(testPlatformConfig))
		tj, err := training.TrainJob(f, training.Context{Namespace: ns, InstanceType: it, Config: pcfg})
		Expect(err).NotTo(HaveOccurred())
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tj)
		Expect(err).NotTo(HaveOccurred())
		u := &unstructured.Unstructured{Object: m}
		unstructured.RemoveNestedField(u.Object, "metadata", "creationTimestamp")
		Expect(k8sClient.Create(ctx, u)).To(Succeed())

		// Kueue admits the job through a Workload labeled with the job's UID.
		wl := &unstructured.Unstructured{}
		wl.SetGroupVersionKind(kf.WorkloadGVK)
		wl.SetNamespace(ns)
		wl.SetName("pause-workload")
		wl.SetLabels(map[string]string{kf.LabelKueueJobUID: string(u.GetUID())})
		Expect(k8sClient.Create(ctx, wl)).To(Succeed())

		activeOf := func() bool {
			got := &unstructured.Unstructured{}
			got.SetGroupVersionKind(kf.WorkloadGVK)
			Expect(get(got, ns, "pause-workload")()).To(Succeed())
			active, found, _ := unstructured.NestedBool(got.Object, "spec", "active")
			return !found || active
		}
		Eventually(activeOf, timeout, interval).Should(BeTrue())

		setStopped := func(stopped bool) {
			Eventually(func() error {
				cur := trainJob(ns, "pause")
				ann := cur.GetAnnotations()
				if ann == nil {
					ann = map[string]string{}
				}
				if stopped {
					ann[training.AnnotationStopped] = naming.LiteralTrue
				} else {
					delete(ann, training.AnnotationStopped)
				}
				cur.SetAnnotations(ann)
				return k8sClient.Update(ctx, cur)
			}, timeout, interval).Should(Succeed())
		}

		// Stopping the job evicts its Workload, which Kueue turns into suspended pods.
		setStopped(true)
		Eventually(activeOf, timeout, interval).Should(BeFalse())

		setStopped(false)
		Eventually(activeOf, timeout, interval).Should(BeTrue())
	})
})

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/inference"
	"github.com/wutz/forgeai/pkg/naming"
	projectv1 "github.com/wutz/project/api/v1alpha1"
)

const (
	inferenceNS      = "infer-test"
	inferenceITName  = "infer-gpu.x1"
	inferenceProduct = "NVIDIA-GeForce-RTX-5090"
)

// inferenceService is a one-GPU service of the test namespace. The
// InstanceType is created by the test: the node-derived ones are pruned
// unless they carry the managed-by label, and this one does not.
func inferenceService(name string) *v1.InferenceService {
	return &v1.InferenceService{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: inferenceNS},
		Spec: v1.InferenceServiceSpec{
			Model:        "Qwen/Qwen3-0.6B",
			InstanceType: inferenceITName,
			Replicas:     1,
		},
	}
}

func getInferenceService(name string) *v1.InferenceService {
	svc := &v1.InferenceService{}
	ExpectWithOffset(1, get(svc, inferenceNS, name)()).To(Succeed())
	return svc
}

func getDeployment(name string) *appsv1.Deployment {
	d := &appsv1.Deployment{}
	ExpectWithOffset(1, get(d, inferenceNS, name)()).To(Succeed())
	return d
}

// setDeploymentReady sets the ready replica count of a Deployment's status,
// as the Deployment controller would once its pods are up.
func setDeploymentReady(name string, ready int32) {
	Eventually(func() error {
		d := getDeployment(name)
		d.Status.Replicas = ready
		d.Status.ReadyReplicas = ready
		d.Status.AvailableReplicas = ready
		return k8sClient.Status().Update(ctx, d)
	}, timeout, interval).Should(Succeed())
}

// setDeploymentFailure sets the ReplicaFailure condition of a Deployment's
// status, as the Deployment controller does when it cannot create a pod.
func setDeploymentFailure(name, msg string) {
	Eventually(func() error {
		d := getDeployment(name)
		now := metav1.Now()
		d.Status.Conditions = append(d.Status.Conditions, appsv1.DeploymentCondition{
			Type: appsv1.DeploymentReplicaFailure, Status: corev1.ConditionTrue, Reason: "FailedCreate",
			Message: msg, LastUpdateTime: now, LastTransitionTime: now,
		})
		return k8sClient.Status().Update(ctx, d)
	}, timeout, interval).Should(Succeed())
}

// readyCondition returns the Ready condition of svc, or nil before the
// reconciler has written one.
func readyCondition(svc *v1.InferenceService) *metav1.Condition {
	return meta.FindStatusCondition(svc.Status.Conditions, inferenceReady)
}

var _ = Describe("InferenceService controller", Ordered, func() {
	BeforeAll(func() {
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: inferenceNS}})).To(Succeed())
		Expect(k8sClient.Create(ctx, &projectv1.InstanceType{
			ObjectMeta: metav1.ObjectMeta{Name: inferenceITName},
			Spec: projectv1.InstanceTypeSpec{
				GPU:          projectv1.InstanceTypeGPU{Class: "rtx-5090", Product: inferenceProduct, Count: 1, MemoryMiB: 32768},
				CPU:          resource.MustParse("8"),
				Memory:       resource.MustParse("64Gi"),
				NodeSelector: map[string]string{naming.LabelGPUProduct: inferenceProduct},
			},
		})).To(Succeed())
	})

	It("renders the model servers, the router and the endpoint, and reports it starting", func() {
		Expect(k8sClient.Create(ctx, inferenceService("qwen"))).To(Succeed())

		Eventually(func() *metav1.Condition {
			return readyCondition(getInferenceService("qwen"))
		}, timeout, interval).ShouldNot(BeNil())

		svc := getInferenceService("qwen")
		Expect(readyCondition(svc).Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCondition(svc).Reason).To(Equal(inferenceStarting))
		Expect(svc.Status.APIKeySecret).To(Equal(naming.InferenceAPIKeySecret("qwen")))
		Expect(svc.Status.Endpoint).To(HavePrefix("https://qwen-" + inferenceNS + ".example.com"))
		Expect(svc.Status.Endpoint).To(HaveSuffix("/v1"))

		model := getDeployment(inference.ModelName(svc))
		Expect(*model.Spec.Replicas).To(Equal(int32(1)))
		Expect(model.OwnerReferences).To(HaveLen(1))
		Expect(model.OwnerReferences[0].Name).To(Equal("qwen"))
		Expect(model.OwnerReferences[0].Controller).To(HaveValue(BeTrue()))

		Expect(getDeployment(inference.EPPName("qwen"))).NotTo(BeNil())

		ing := &networkingv1.Ingress{}
		Expect(get(ing, inferenceNS, "qwen")()).To(Succeed())
		Expect(ing.Spec.Rules).NotTo(BeEmpty())
		Expect(ing.Spec.Rules[0].Host).To(Equal("qwen-" + inferenceNS + ".example.com"))

		// The API key is created once, with the service.
		key := &corev1.Secret{}
		Expect(get(key, inferenceNS, naming.InferenceAPIKeySecret("qwen"))()).To(Succeed())
		Expect(key.Data[naming.InferenceAPIKey]).NotTo(BeEmpty())
	})

	It("reports Ready once the model servers and the router are ready", func() {
		setDeploymentReady(inference.ModelName(getInferenceService("qwen")), 1)
		// The router needs its Endpoint Picker too: a model server alone is not
		// enough to route a request.
		setDeploymentReady(inference.EPPName("qwen"), 1)
		Eventually(func() string {
			return readyCondition(getInferenceService("qwen")).Reason
		}, timeout, interval).Should(Equal(inferenceReady))

		svc := getInferenceService("qwen")
		Expect(readyCondition(svc).Status).To(Equal(metav1.ConditionTrue))
		Expect(svc.Status.ReadyReplicas).To(Equal(int32(1)))
	})

	It("keeps the API key when the service is re-rendered", func() {
		before := &corev1.Secret{}
		Expect(get(before, inferenceNS, naming.InferenceAPIKeySecret("qwen"))()).To(Succeed())

		// A spec change bumps the generation and reconciles the objects again.
		Eventually(func() error {
			svc := getInferenceService("qwen")
			svc.Spec.Args = []string{"--max-model-len", "4096"}
			return k8sClient.Update(ctx, svc)
		}, timeout, interval).Should(Succeed())
		Eventually(func() []string {
			return getDeployment(inference.ModelName(getInferenceService("qwen"))).
				Spec.Template.Spec.Containers[0].Args
		}, timeout, interval).Should(ContainElements("--max-model-len", "4096"))

		after := &corev1.Secret{}
		Expect(get(after, inferenceNS, naming.InferenceAPIKeySecret("qwen"))()).To(Succeed())
		Expect(after.Data[naming.InferenceAPIKey]).To(Equal(before.Data[naming.InferenceAPIKey]))
	})

	It("scales the model servers and the router to zero when stopped, and back when started", func() {
		Eventually(func() error {
			svc := getInferenceService("qwen")
			svc.Annotations = map[string]string{naming.AnnotationStopped: naming.LiteralTrue}
			return k8sClient.Update(ctx, svc)
		}, timeout, interval).Should(Succeed())

		Eventually(func() string {
			return readyCondition(getInferenceService("qwen")).Reason
		}, timeout, interval).Should(Equal(inferenceStopped))
		Eventually(func() int32 {
			return *getDeployment(inference.ModelName(getInferenceService("qwen"))).Spec.Replicas
		}, timeout, interval).Should(BeZero())
		Eventually(func() int32 {
			return *getDeployment(inference.EPPName("qwen")).Spec.Replicas
		}, timeout, interval).Should(BeZero())
		// No Deployment controller runs here: the pods go away only in the status
		// the test writes, as they would in a cluster.
		setDeploymentReady(inference.ModelName(getInferenceService("qwen")), 0)
		setDeploymentReady(inference.EPPName("qwen"), 0)

		Eventually(func() error {
			svc := getInferenceService("qwen")
			delete(svc.Annotations, naming.AnnotationStopped)
			return k8sClient.Update(ctx, svc)
		}, timeout, interval).Should(Succeed())

		Eventually(func() int32 {
			return *getDeployment(inference.ModelName(getInferenceService("qwen"))).Spec.Replicas
		}, timeout, interval).Should(Equal(int32(1)))
		Eventually(func() string {
			return readyCondition(getInferenceService("qwen")).Reason
		}, timeout, interval).Should(Equal(inferenceStarting))
	})

	It("reports an InstanceType without GPUs and renders nothing", func() {
		Expect(k8sClient.Create(ctx, &projectv1.InstanceType{
			ObjectMeta: metav1.ObjectMeta{Name: "infer-cpu.x1"},
			Spec: projectv1.InstanceTypeSpec{
				CPU:          resource.MustParse("4"),
				Memory:       resource.MustParse("8Gi"),
				NodeSelector: map[string]string{naming.LabelGPUProduct: "cpu"},
			},
		})).To(Succeed())
		svc := inferenceService("cpu-only")
		svc.Spec.InstanceType = "infer-cpu.x1"
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())

		Eventually(func() string {
			c := readyCondition(getInferenceService("cpu-only"))
			if c == nil {
				return ""
			}
			return c.Reason
		}, timeout, interval).Should(Equal(inferenceNoGPU))
		Expect(get(&appsv1.Deployment{}, inferenceNS, inference.ModelName(svc))()).NotTo(Succeed())
	})

	It("reports a missing InstanceType and renders nothing", func() {
		svc := inferenceService("missing-it")
		svc.Spec.InstanceType = "no-such-gpu.x1"
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())

		Eventually(func() string {
			c := readyCondition(getInferenceService("missing-it"))
			if c == nil {
				return ""
			}
			return c.Reason
		}, timeout, interval).Should(Equal(inferenceInstanceMissing))

		err := get(&appsv1.Deployment{}, inferenceNS, inference.ModelName(svc))()
		Expect(err).To(HaveOccurred())
	})

	It("reports why a model server cannot start, such as a refused quota", func() {
		svc := inferenceService("quota")
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())
		Eventually(func() *metav1.Condition {
			return readyCondition(getInferenceService("quota"))
		}, timeout, interval).ShouldNot(BeNil())

		refusal := `pods "quota-model-0" is forbidden: exceeded quota: forgeai-quota, requested: requests.cpu=4`
		setDeploymentFailure(inference.ModelName(svc), refusal)

		Eventually(func() string {
			return readyCondition(getInferenceService("quota")).Message
		}, timeout, interval).Should(ContainSubstring(refusal))
		c := readyCondition(getInferenceService("quota"))
		Expect(c.Status).To(Equal(metav1.ConditionFalse))
		Expect(c.Reason).To(Equal(inferenceStarting))
	})
})

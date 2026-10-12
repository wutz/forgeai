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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/kf"
	"github.com/scclabs/forgeai/pkg/naming"
)

const testPlatformConfig = `
releaseVersion: test
rootDomain: dev1.example.com
ingressClass: nginx
defaultQueue: forgeai-queue
resources:
  - {id: g1, gpu: {count: 1}, cpu: {cores: 7}, memory: {sizeGB: 100}, labels: {scclabs.dev/gpu: rtx-5090}}
`

const (
	timeout  = 20 * time.Second
	interval = 200 * time.Millisecond
)

func newTrainingJob(userID, jobID, resourceID string) *v1.TrainingJob {
	return &v1.TrainingJob{
		ObjectMeta: metav1.ObjectMeta{Name: naming.JobName(jobNamespace, userID, jobID), Namespace: jobNamespace},
		Spec: v1.TrainingJobSpec{
			UserID: userID, JobID: jobID, Image: "img:1", Command: "sleep 1",
			ResourceID: resourceID, Workers: 2,
		},
	}
}

func getTJ(name string) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(kf.TrainJobGVK)
	err := k8sClient.Get(ctx, types.NamespacedName{Namespace: jobNamespace, Name: name}, u)
	return u, err
}

// finishDeletion emulates the garbage collector, which envtest does not run:
// a foreground delete leaves the TrainJob with a finalizer.
func finishDeletion(name string) {
	Eventually(func(g Gomega) {
		u, err := getTJ(name)
		if apierrors.IsNotFound(err) {
			return
		}
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(u.GetDeletionTimestamp()).NotTo(BeNil())
		u.SetFinalizers(nil)
		g.Expect(k8sClient.Update(ctx, u)).To(Succeed())
	}, timeout, interval).Should(Succeed())
}

func phaseOf(key types.NamespacedName) func() v1.TrainingJobPhase {
	return func() v1.TrainingJobPhase {
		j := &v1.TrainingJob{}
		if err := k8sClient.Get(ctx, key, j); err != nil {
			return ""
		}
		return j.Status.Phase
	}
}

func countOwned(list client.ObjectList, name string) int {
	ExpectWithOffset(1, k8sClient.List(ctx, list, client.InNamespace(jobNamespace),
		client.MatchingLabels{naming.LabelTrainingJob: name})).To(Succeed())
	switch l := list.(type) {
	case *corev1.ServiceList:
		return len(l.Items)
	case *networkingv1.IngressList:
		return len(l.Items)
	case *unstructured.UnstructuredList:
		return len(l.Items)
	}
	return -1
}

func pipeList() *unstructured.UnstructuredList {
	l := &unstructured.UnstructuredList{}
	l.SetGroupVersionKind(kf.PipeGVK.GroupVersion().WithKind("PipeList"))
	return l
}

var _ = Describe("TrainingJob Controller", func() {
	It("creates, completes, restarts and stops a job", func() {
		job := newTrainingJob("user-a", "ackjob-1", "g1")
		key := client.ObjectKeyFromObject(job)
		Expect(k8sClient.Create(ctx, job)).To(Succeed())

		By("rendering the TrainJob, runtime, secret and access objects")
		var firstUID types.UID
		Eventually(func(g Gomega) {
			u, err := getTJ(job.Name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(u.GetLabels()[kf.LabelKueueQueue]).To(Equal("forgeai-queue"))
			firstUID = u.GetUID()
		}, timeout, interval).Should(Succeed())

		rt := &unstructured.Unstructured{}
		rt.SetGroupVersionKind(kf.TrainingRuntimeGVK)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: jobNamespace, Name: naming.RuntimeName(job.Name)}, rt)).To(Succeed())
		nodes, _, _ := unstructured.NestedInt64(rt.Object, "spec", "mlPolicy", "numNodes")
		Expect(nodes).To(BeEquivalentTo(2))

		secret := &corev1.Secret{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: jobNamespace, Name: naming.SecretName(job.Name)}, secret)
		}, timeout, interval).Should(Succeed())
		password := string(secret.Data["SSH_PASSWORD"])
		Expect(password).To(HaveLen(32))

		Eventually(func() int { return countOwned(&corev1.ServiceList{}, job.Name) }, timeout, interval).Should(Equal(4))
		Eventually(func() int { return countOwned(&networkingv1.IngressList{}, job.Name) }, timeout, interval).Should(Equal(2))
		Eventually(func() int { return countOwned(pipeList(), job.Name) }, timeout, interval).Should(Equal(2))
		Eventually(phaseOf(key), timeout, interval).Should(Equal(v1.PhasePending))

		got := &v1.TrainingJob{}
		Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
		Expect(got.Status.Workers).To(HaveLen(2))
		Expect(got.Status.Workers[1].SSHUser).To(Equal("root@ackjob-1-1"))

		By("mirroring TrainJob completion")
		u, err := getTJ(job.Name)
		Expect(err).NotTo(HaveOccurred())
		Expect(unstructured.SetNestedSlice(u.Object, []any{map[string]any{
			"type": "Complete", "status": "True", "reason": "AllJobsCompleted", "message": "done",
			"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
		}}, "status", "conditions")).To(Succeed())
		Expect(k8sClient.Status().Update(ctx, u)).To(Succeed())
		Eventually(phaseOf(key), timeout, interval).Should(Equal(v1.PhaseSucceeded))

		By("restarting: the TrainJob is deleted then recreated")
		Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
		got.Spec.RestartGeneration++
		Expect(k8sClient.Update(ctx, got)).To(Succeed())
		finishDeletion(job.Name)
		Eventually(func(g Gomega) {
			u, err := getTJ(job.Name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(u.GetUID()).NotTo(Equal(firstUID))
			g.Expect(u.GetDeletionTimestamp()).To(BeNil())
		}, timeout, interval).Should(Succeed())
		Eventually(func() int64 {
			j := &v1.TrainingJob{}
			_ = k8sClient.Get(ctx, key, j)
			return j.Status.ObservedRestartGeneration
		}, timeout, interval).Should(BeEquivalentTo(1))
		Eventually(phaseOf(key), timeout, interval).Should(Equal(v1.PhasePending))

		By("stopping: TrainJob and access objects go away, secret is kept")
		Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
		got.Spec.Suspend = true
		Expect(k8sClient.Update(ctx, got)).To(Succeed())
		finishDeletion(job.Name)
		Eventually(phaseOf(key), timeout, interval).Should(Equal(v1.PhaseStopped))
		Eventually(func() int { return countOwned(&corev1.ServiceList{}, job.Name) }, timeout, interval).Should(Equal(0))
		Eventually(func() int { return countOwned(pipeList(), job.Name) }, timeout, interval).Should(Equal(0))
		Expect(countOwned(&networkingv1.IngressList{}, job.Name)).To(Equal(0))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: jobNamespace, Name: naming.SecretName(job.Name)}, secret)).To(Succeed())
		Expect(string(secret.Data["SSH_PASSWORD"])).To(Equal(password))

		By("starting again")
		Expect(k8sClient.Get(ctx, key, got)).To(Succeed())
		got.Spec.Suspend = false
		Expect(k8sClient.Update(ctx, got)).To(Succeed())
		Eventually(func() error { _, err := getTJ(job.Name); return err }, timeout, interval).Should(Succeed())
		Eventually(func() int { return countOwned(pipeList(), job.Name) }, timeout, interval).Should(Equal(2))
	})

	It("reports an unknown resource model", func() {
		job := newTrainingJob("user-b", "ackjob-2", "missing")
		Expect(k8sClient.Create(ctx, job)).To(Succeed())
		key := client.ObjectKeyFromObject(job)
		Eventually(phaseOf(key), timeout, interval).Should(Equal(v1.PhaseFailed))
		_, err := getTJ(job.Name)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
})

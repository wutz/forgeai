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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/naming"
)

var _ = Describe("PoolRequest Controller", func() {
	It("holds nodes with a placeholder Job, then moves them to the destination pool", func() {
		node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:   "gpu-node-1",
			Labels: map[string]string{"scclabs.dev/gpu": "rtx-5090", naming.LabelNodePool: "src"},
		}}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())

		pr := &v1.PoolRequest{
			ObjectMeta: metav1.ObjectMeta{Name: naming.PoolRequestName(jobNamespace, "1001", "req-1"), Namespace: jobNamespace},
			Spec: v1.PoolRequestSpec{
				UserID: "1001", RequestID: "req-1", SrcPool: "src", DstPool: "dst", ResourceID: "g1", NNodes: 1,
			},
		}
		Expect(k8sClient.Create(ctx, pr)).To(Succeed())
		key := types.NamespacedName{Namespace: jobNamespace, Name: pr.Name}

		By("creating a placeholder Job pinned to the source pool")
		job := &batchv1.Job{}
		Eventually(func() error { return k8sClient.Get(ctx, key, job) }, timeout, interval).Should(Succeed())
		spec := job.Spec.Template.Spec
		Expect(spec.NodeSelector).To(HaveKeyWithValue(naming.LabelNodePool, "src"))
		Expect(spec.NodeSelector).To(HaveKeyWithValue("scclabs.dev/gpu", "rtx-5090"))
		Expect(spec.Containers[0].Image).To(Equal("pause:test"))
		Expect(spec.Containers[0].Resources.Requests[corev1.ResourceName("nvidia.com/gpu")]).To(Equal(resource.MustParse("1")))
		Expect(job.Labels).To(HaveKeyWithValue(naming.LabelComponent, naming.ComponentPoolPlaceholder))
		Eventually(func() v1.PoolRequestPhase {
			got := &v1.PoolRequest{}
			_ = k8sClient.Get(ctx, key, got)
			return got.Status.Phase
		}, timeout, interval).Should(Equal(v1.PoolRequestPending))

		By("reporting success once the placeholder pod runs")
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: pr.Name + "-0", Namespace: jobNamespace, Labels: job.Spec.Template.Labels},
			Spec: corev1.PodSpec{
				NodeName:   node.Name,
				Containers: []corev1.Container{{Name: "placeholder", Image: "pause:test"}},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		pod.Status.Phase = corev1.PodRunning
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

		got := &v1.PoolRequest{}
		Eventually(func() v1.PoolRequestPhase {
			_ = k8sClient.Get(ctx, key, got)
			return got.Status.Phase
		}, timeout, interval).Should(Equal(v1.PoolRequestSuccess))
		Expect(got.Status.Nodes).To(Equal([]string{node.Name}))
		Expect(got.Status.CompletionTime).NotTo(BeNil())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: node.Name}, node)).To(Succeed())
		Expect(node.Labels).To(HaveKeyWithValue(naming.LabelNodePool, "dst"))

		By("releasing the placeholder Job")
		Eventually(func() bool {
			err := k8sClient.Get(ctx, key, job)
			return apierrors.IsNotFound(err) || !job.DeletionTimestamp.IsZero()
		}, timeout, interval).Should(BeTrue())

		Expect(k8sClient.Delete(ctx, pod, client.GracePeriodSeconds(0))).To(Succeed())
		Expect(k8sClient.Delete(ctx, pr)).To(Succeed())
		Expect(k8sClient.Delete(ctx, node)).To(Succeed())
	})

	It("fails requests for unknown resources", func() {
		pr := &v1.PoolRequest{
			ObjectMeta: metav1.ObjectMeta{Name: naming.PoolRequestName(jobNamespace, "1001", "req-2"), Namespace: jobNamespace},
			Spec:       v1.PoolRequestSpec{UserID: "1001", RequestID: "req-2", DstPool: "dst", ResourceID: "nope"},
		}
		Expect(k8sClient.Create(ctx, pr)).To(Succeed())
		got := &v1.PoolRequest{}
		Eventually(func() v1.PoolRequestPhase {
			_ = k8sClient.Get(ctx, client.ObjectKeyFromObject(pr), got)
			return got.Status.Phase
		}, timeout, interval).Should(Equal(v1.PoolRequestFailed))
		Expect(got.Status.Message).NotTo(BeEmpty())
		Expect(k8sClient.Delete(ctx, pr)).To(Succeed())
	})
})

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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/scclabs/forgeai/pkg/images"
	"github.com/scclabs/forgeai/pkg/platform"
)

var _ = Describe("ImageWarmup Controller", func() {
	It("deletes expired warmup DaemonSets", func() {
		cfg, err := platform.Parse([]byte(testPlatformConfig))
		Expect(err).NotTo(HaveOccurred())
		ds := images.DaemonSet(jobNamespace, "img:expired", cfg, time.Now().Add(-time.Minute))
		Expect(k8sClient.Create(ctx, ds)).To(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ds), &appsv1.DaemonSet{})
			return apierrors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
	})

	It("keeps DaemonSets that have not pulled yet", func() {
		cfg, err := platform.Parse([]byte(testPlatformConfig))
		Expect(err).NotTo(HaveOccurred())
		ds := images.DaemonSet(jobNamespace, "img:pending", cfg, time.Now().Add(time.Hour))
		Expect(k8sClient.Create(ctx, ds)).To(Succeed())
		Consistently(func() error {
			return k8sClient.Get(ctx, client.ObjectKeyFromObject(ds), &appsv1.DaemonSet{})
		}, 2*time.Second, interval).Should(Succeed())
		Expect(k8sClient.Delete(ctx, ds)).To(Succeed())
	})
})

var _ = DescribeTable("imagePulled",
	func(s corev1.ContainerState, want bool) {
		p := &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{State: s}}}}
		Expect(imagePulled(p)).To(Equal(want))
	},
	Entry("running", corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}, true),
	Entry("pulling", corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}, false),
	Entry("pull backoff", corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}, false),
	Entry("pulled but crashing", corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}, true),
)

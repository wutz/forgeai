package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/wutz/forgeai/pkg/sandbox"
)

var _ = Describe("ignoreGatewayDelete", func() {
	owned := func(name, kind string) *corev1.Service {
		svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "forge-qa-test"}}
		svc.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: sandbox.SandboxGVK.GroupVersion().String(),
			Kind:       kind,
			Name:       "gw",
			UID:        "uid-gw",
			Controller: new(true),
		}}
		return svc
	}
	passes := func(obj *corev1.Service) bool {
		return ignoreGatewayDelete().Delete(event.DeleteEvent{Object: obj})
	}

	It("drops the delete of the sandbox's gateway Service", func() {
		Expect(passes(owned(sandbox.GatewayServiceName("gw"), sandbox.SandboxGVK.Kind))).To(BeFalse())
	})

	It("keeps the delete of the sandbox's other Services", func() {
		Expect(passes(owned("gw", sandbox.SandboxGVK.Kind))).To(BeTrue())
		Expect(passes(owned("gw-ssh", sandbox.SandboxGVK.Kind))).To(BeTrue())
	})

	It("keeps the delete of a Service not owned by a sandbox", func() {
		svc := owned(sandbox.GatewayServiceName("gw"), "Deployment")
		Expect(passes(svc)).To(BeTrue())
		Expect(passes(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: sandbox.GatewayServiceName("gw")}})).To(BeTrue())
	})
})

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
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/sandbox"
	"github.com/wutz/forgeai/pkg/sshgateway"
)

// Keys of the platform identity Secret. The gateway logs in to sandboxes with
// the private key; sandboxes accept the public key only.
const (
	sshPrivateKeyKey = "ssh-privatekey"
	sshPublicKeyKey  = "ssh-publickey"
)

// ensureSSH gives an SSH sandbox the Service that reaches its sshd sidecar and
// the Secret that holds the sshd host key and the platform public key. Both are
// owned by the sandbox and go with it.
func (r *SandboxReconciler) ensureSSH(ctx context.Context, u *unstructured.Unstructured) error {
	ns, name := u.GetNamespace(), u.GetName()
	owner := []metav1.OwnerReference{{
		APIVersion: sandbox.SandboxGVK.GroupVersion().String(),
		Kind:       sandbox.SandboxGVK.Kind,
		Name:       name,
		UID:        u.GetUID(),
		Controller: new(true),
	}}
	labels := map[string]string{
		naming.LabelManagedBy: naming.ManagedBy,
		naming.LabelComponent: naming.ComponentSandbox,
		naming.LabelProject:   u.GetLabels()[naming.LabelProject],
	}

	identity, err := ensureSSHIdentity(ctx, r.Client, platform.Namespace())
	if err != nil {
		return err
	}

	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: naming.SSHServiceName(name)}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Labels = labels
		svc.OwnerReferences = owner
		svc.Spec.Selector = map[string]string{naming.LabelSandbox: name}
		// Every field the API server would default is set here too, so that an
		// unchanged Service is not updated again on each reconcile.
		svc.Spec.Ports = []corev1.ServicePort{{
			Name:       sshPortName,
			Port:       sandbox.SSHPort,
			TargetPort: intstr.FromInt32(sandbox.SSHPort),
			Protocol:   corev1.ProtocolTCP,
		}}
		return nil
	}); err != nil {
		return err
	}

	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: naming.SSHHostKeySecret(naming.SSHServiceName(name))}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, sec, func() error {
		sec.Labels = labels
		sec.OwnerReferences = owner
		sec.Type = corev1.SecretTypeOpaque
		// The host key is generated once: a new one would fail every client that
		// has pinned the old one.
		hostKey := sec.Data[sshgateway.SSHHostKeyKey]
		if len(hostKey) == 0 {
			priv, _, err := sshgateway.GenerateEd25519()
			if err != nil {
				return err
			}
			hostKey = priv
		}
		sec.Data = map[string][]byte{
			sshgateway.SSHHostKeyKey:       hostKey,
			sshgateway.SSHAuthorizedKeyKey: identity.Data[sshPublicKeyKey],
		}
		return nil
	})
	return err
}

// ensureSSHIdentity returns the platform identity Secret, generating its key pair
// on first use.
func ensureSSHIdentity(ctx context.Context, c client.Client, namespace string) (*corev1.Secret, error) {
	key := types.NamespacedName{Namespace: namespace, Name: naming.SSHIdentitySecretName}
	existing := &corev1.Secret{}
	err := c.Get(ctx, key, existing)
	if err == nil {
		return existing, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}
	priv, pub, err := sshgateway.GenerateEd25519()
	if err != nil {
		return nil, err
	}
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      naming.SSHIdentitySecretName,
			Labels: map[string]string{
				naming.LabelManagedBy: naming.ManagedBy,
				naming.LabelComponent: naming.ComponentSSHGateway,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{sshPrivateKeyKey: priv, sshPublicKeyKey: []byte(pub + "\n")},
	}
	if err := c.Create(ctx, s); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Another reconcile created it first: its keys are the ones in use.
			if err := c.Get(ctx, key, existing); err != nil {
				return nil, err
			}
			return existing, nil
		}
		return nil, err
	}
	return s, nil
}

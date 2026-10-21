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
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
)

// The image has no shell, so dufs must be started directly. The root emptyDir
// is writable at the mount level so runc can create the volume mountpoints in
// it, and dufs' auth rules keep everything except the volumes read-only.
func TestFileServerServesVolumesWritableAndRootReadOnly(t *testing.T) {
	fs := &v1.FileServer{ObjectMeta: metav1.ObjectMeta{Name: naming.FileServerName, Namespace: "forge-demo"}}
	cfg := &platform.Config{}
	cfg.FileServer.Image = "docker.io/sigoden/dufs:v0.46.0"
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: naming.FileServerSecretName}}
	vols := []string{"data", "shared"}

	spec := fileServerDeployment(fs, cfg, "forgeai-files-default", nil, vols, secret, 1).Spec.Template.Spec

	if len(spec.InitContainers) != 0 {
		t.Errorf("got %d init containers, want none: the image has no shell to run them", len(spec.InitContainers))
	}
	main := spec.Containers[0]
	if !slices.Equal(main.Command, []string{"dufs"}) {
		t.Errorf("command %v, want dufs without a shell", main.Command)
	}
	if got, want := argValue(main.Args, "--auth"), "forgeai:$(DUFS_PASSWORD)@/:ro,/data:rw,/shared:rw"; got != want {
		t.Errorf("auth rule %q, want %q", got, want)
	}

	root := findMount(main.VolumeMounts, rootVolumeName)
	if root == nil || root.MountPath != "/data" || root.ReadOnly {
		t.Errorf("root must be mounted writable at /data so runc can create mountpoints, got %+v", root)
	}
	for _, v := range vols {
		m := findMount(main.VolumeMounts, "v-"+v)
		if m == nil || m.MountPath != "/data/"+v {
			t.Errorf("volume %q not mounted at /data/%s: %+v", v, v, m)
		}
	}
}

func TestFileServerWithoutVolumesGrantsNoWrites(t *testing.T) {
	fs := &v1.FileServer{ObjectMeta: metav1.ObjectMeta{Name: naming.FileServerName, Namespace: "forge-demo"}}
	cfg := &platform.Config{}
	cfg.FileServer.Image = "docker.io/sigoden/dufs:v0.46.0"
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: naming.FileServerSecretName}}

	spec := fileServerDeployment(fs, cfg, "forgeai-files-default", nil, nil, secret, 0).Spec.Template.Spec
	if got, want := argValue(spec.Containers[0].Args, "--auth"), "forgeai:$(DUFS_PASSWORD)@/:ro"; got != want {
		t.Errorf("auth rule %q, want %q", got, want)
	}
}

func argValue(args []string, flag string) string {
	for i := range args {
		if args[i] == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func findMount(mounts []corev1.VolumeMount, name string) *corev1.VolumeMount {
	for i := range mounts {
		if mounts[i].Name == name {
			return &mounts[i]
		}
	}
	return nil
}

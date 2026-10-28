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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	forgeaiv1alpha1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/platform"
	projectv1 "github.com/wutz/project/api/v1alpha1"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

const configNamespace = "forgeai-system"

// projectModule is the module of the project operator. It owns the Project and
// InstanceType CRDs, and its reconcilers are not run here: the tests create the
// InstanceType objects they need.
const projectModule = "github.com/wutz/project"

// projectCRDDir returns the CRD directory of the project module this build depends on.
func projectCRDDir() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", projectModule).Output()
	if err != nil {
		return "", fmt.Errorf("locate %s: %w", projectModule, err)
	}
	return filepath.Join(strings.TrimSpace(string(out)), "config", "crd", "bases"), nil
}

const testPlatformConfig = `
namespacePrefix: forge-
apiServer: https://api.example.com:6443
ingress: {className: nginx, domain: example.com, port: 7443}
gateway: {name: forgeai-sandbox, namespace: forgeai-system, scheme: http, port: 8080}
registry: {domain: cr.example.com:7443}
storageTypes:
  shared: {storageClass: shared}
defaultStorageType: shared
instanceTypes:
  reserved: {cpu: "8", memory: 64Gi}
  sizes: [1, 2, 4, 8]
defaultQuota:
  gpus: {"*": 4}
  storage: 1Ti
network:
  systemNamespaces: [ingress-nginx]
sandbox:
  idleTimeout: 1h
training:
  stuckTimeout: 1s
trashRetention: 1h
`

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	err = forgeaiv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = projectv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	projectCRDs, err := projectCRDDir()
	Expect(err).NotTo(HaveOccurred())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			projectCRDs,
			// Trainer and Kueue CRDs exported from the dev cluster.
			filepath.Join("testdata", "crds"),
		},
		ErrorIfCRDPathMissing: true,
	}

	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: configNamespace}})).To(Succeed())
	Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: platform.ConfigMapName, Namespace: configNamespace},
		Data:       map[string]string{platform.ConfigKey: testPlatformConfig},
	})).To(Succeed())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme.Scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		Client:  client.Options{Cache: &client.CacheOptions{Unstructured: true}},
	})
	Expect(err).NotTo(HaveOccurred())
	loader := &platform.Loader{Reader: mgr.GetAPIReader(), Namespace: configNamespace}
	for _, r := range []interface{ SetupWithManager(ctrl.Manager) error }{
		&PersistentVolumeClaimReconciler{Client: mgr.GetClient(), Config: loader},
		&TrainJobReconciler{Client: mgr.GetClient(), Config: loader, Recorder: mgr.GetEventRecorder("forgeai-test")},
		&InferenceServiceReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Config: loader},
		&SandboxReconciler{Client: mgr.GetClient(), APIReader: mgr.GetAPIReader(), Config: loader, Gateway: true},
	} {
		Expect(r.SetupWithManager(mgr)).To(Succeed())
	}

	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Makefile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

package controller

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	forgeaiv1alpha1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	"github.com/wutz/forgeai/pkg/training"
	projectv1 "github.com/wutz/workspace/api/v1alpha1"
)

// TestTrainJobWithoutKueue runs the TrainJob controller in an API server that
// has no Kueue CRDs. The controller must set up its watches there, and stopping
// a job must set the job's own suspend flag, since there is no Workload to
// deactivate. The shared suite loads the Kueue CRDs, so this test starts its own
// environment.
func TestTrainJobWithoutKueue(t *testing.T) {
	RegisterTestingT(t)

	projectCRDs, err := projectCRDDir()
	if err != nil {
		t.Fatal(err)
	}
	trainerCRDs := t.TempDir()
	copyCRDsExcept(t, filepath.Join("testdata", "crds"), trainerCRDs, "kueue.x-k8s.io.json")

	env := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			projectCRDs,
			trainerCRDs,
		},
		ErrorIfCRDPathMissing: true,
	}
	if dir := getFirstFoundEnvTestBinaryDir(); dir != "" {
		env.BinaryAssetsDirectory = dir
	}
	restCfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := env.Stop(); err != nil {
			t.Errorf("stop envtest: %v", err)
		}
	}()

	Expect(forgeaiv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(projectv1.AddToScheme(scheme.Scheme)).To(Succeed())
	c, err := client.New(restCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	// The helpers in the suite read the package-level client and context.
	k8sClient = c
	ctx = context.Background()

	installed, err := kf.Installed(c.RESTMapper(), kf.WorkloadGVK)
	Expect(err).NotTo(HaveOccurred())
	Expect(installed).To(BeFalse(), "Kueue must be absent in this environment")

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:  scheme.Scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		// The suite registers the same controller name in this process.
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	})
	Expect(err).NotTo(HaveOccurred())
	r := &TrainJobReconciler{
		Client: c,
		Config: &platform.Loader{Reader: c, Namespace: configNamespace},
	}
	Expect(r.SetupWithManager(mgr)).To(Succeed(), "the controller must start without Kueue")

	Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: configNamespace}})).To(Succeed())
	Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: platform.ConfigMapName, Namespace: configNamespace},
		Data:       map[string]string{platform.ConfigKey: testPlatformConfig},
	})).To(Succeed())
	const ns = "forge-nokueue"
	Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: ns, Labels: map[string]string{naming.LabelProject: "nokueue"},
	}})).To(Succeed())
	createInstanceType("rtx-5090.x1", 1, "14", "117Gi", 16)

	pcfg, err := platform.Parse([]byte(testPlatformConfig))
	Expect(err).NotTo(HaveOccurred())
	it := &projectv1.InstanceType{}
	Expect(get(it, "", "rtx-5090.x1")()).To(Succeed())
	f := &training.Form{Name: "pause", Image: "img", InstanceType: "rtx-5090.x1", Workers: 1}
	tj, err := training.TrainJob(f, training.Context{Namespace: ns, InstanceType: it, Config: pcfg})
	Expect(err).NotTo(HaveOccurred())
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tj)
	Expect(err).NotTo(HaveOccurred())
	u := &unstructured.Unstructured{Object: m}
	unstructured.RemoveNestedField(u.Object, "metadata", "creationTimestamp")
	Expect(k8sClient.Create(ctx, u)).To(Succeed())

	reconcile := func() {
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "pause"}})
		Expect(err).NotTo(HaveOccurred())
	}
	setStopped := func(stopped bool) {
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
		Expect(k8sClient.Update(ctx, cur)).To(Succeed())
	}
	suspended := func() bool {
		v, found, _ := unstructured.NestedBool(trainJob(ns, "pause").Object, naming.KeySpec, "suspend")
		return found && v
	}

	setStopped(true)
	reconcile()
	Expect(suspended()).To(BeTrue(), "a stopped job must be suspended without Kueue")

	setStopped(false)
	reconcile()
	Expect(suspended()).To(BeFalse(), "a resumed job must be unsuspended without Kueue")
}

// copyCRDsExcept copies the CRD files of src to dst, leaving out those whose
// names contain skip.
func copyCRDsExcept(t *testing.T, src, dst, skip string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), skip) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

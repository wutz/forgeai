package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/wutz/forgeai/api/v1alpha1"
	"github.com/wutz/forgeai/pkg/inference"
	"github.com/wutz/forgeai/pkg/naming"
)

// modelPattern is the Hugging Face repository form the InferenceService CRD accepts.
var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)

// InferenceService is the view model of an InferenceService (forgeai.wutz.dev).
type InferenceService struct {
	Name              string   `json:"name"`
	Model             string   `json:"model"`
	ServedName        string   `json:"servedName"`
	InstanceType      string   `json:"instanceType"`
	Replicas          int32    `json:"replicas"`
	ReadyReplicas     int32    `json:"readyReplicas"`
	Endpoint          string   `json:"endpoint,omitempty"`
	APIKeySecret      string   `json:"apiKeySecret,omitempty"`
	Args              []string `json:"args,omitempty"`
	HuggingFaceSecret string   `json:"huggingFaceSecret,omitempty"`
	// Reason and Message are the Ready condition: Ready, Starting, Stopped or a failure.
	Reason  string `json:"reason"`
	Message string `json:"message,omitempty"`
	// Ready is set when the Ready condition is True.
	Ready   bool   `json:"ready"`
	Stopped bool   `json:"stopped"`
	Created string `json:"created"`
	Age     string `json:"age"`
}

// InferenceForm is the "New inference service" form.
type InferenceForm struct {
	Name              string   `json:"name"`
	Model             string   `json:"model"`
	ServedName        string   `json:"servedName"`
	InstanceType      string   `json:"instanceType"`
	Replicas          int32    `json:"replicas"`
	Args              []string `json:"args"`
	HuggingFaceSecret string   `json:"huggingFaceSecret"`
}

// ListInferenceServices lists the InferenceServices of the project, newest first.
func (a *App) ListInferenceServices() ([]*InferenceService, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	list := &v1.InferenceServiceList{}
	if err := c.k8s.List(ctx, list, client.InNamespace(c.namespace())); err != nil {
		if meta.IsNoMatchError(err) {
			return []*InferenceService{}, nil
		}
		return nil, err
	}
	out := make([]*InferenceService, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, toInferenceService(&list.Items[i]))
	}
	return out, nil
}

// GetInferenceService returns one InferenceService.
func (a *App) GetInferenceService(name string) (*InferenceService, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	svc, err := getInferenceService(ctx, c, name)
	if err != nil {
		return nil, err
	}
	return toInferenceService(svc), nil
}

// CreateInferenceService creates an InferenceService from the form. The
// operator then serves the model; the view shows its progress.
func (a *App) CreateInferenceService(f InferenceForm) (*InferenceService, error) {
	c, err := a.current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.call()
	defer cancel()
	f = normalizeInferenceForm(f)
	if err := checkInferenceForm(f, c.namespace()); err != nil {
		return nil, err
	}
	it, err := lookupInstanceType(ctx, c, f.InstanceType)
	if err != nil {
		return nil, err
	}
	if it == nil {
		return nil, fmt.Errorf("instance type %q not found", f.InstanceType)
	}
	if it.Spec.GPU.Count < 1 {
		return nil, fmt.Errorf("instance type %q has no GPUs; a model needs at least one", f.InstanceType)
	}
	svc := renderInferenceService(f, c.namespace())
	if err := c.k8s.Create(ctx, svc); err != nil {
		return nil, err
	}
	return toInferenceService(svc), nil
}

// StopInferenceService scales the model servers and the router to zero. The
// object and its API key are kept, so the service can be started again.
func (a *App) StopInferenceService(name string) (*InferenceService, error) {
	return a.mutateInferenceService(name, func(svc *v1.InferenceService) {
		setAnnotation(svc, naming.AnnotationStopped, naming.LiteralTrue)
	})
}

// StartInferenceService scales a stopped InferenceService back up.
func (a *App) StartInferenceService(name string) (*InferenceService, error) {
	return a.mutateInferenceService(name, func(svc *v1.InferenceService) {
		delete(svc.Annotations, naming.AnnotationStopped)
	})
}

// DeleteInferenceService deletes an InferenceService. The operator's objects
// (router, model servers, Ingress and API key) go with it through their owner
// references.
func (a *App) DeleteInferenceService(name string) error {
	c, err := a.current()
	if err != nil {
		return err
	}
	ctx, cancel := a.call()
	defer cancel()
	svc, err := getInferenceService(ctx, c, name)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return c.k8s.Delete(ctx, svc)
}

// InferenceAPIKey returns the API key clients send as a bearer token to the
// endpoint of an InferenceService.
func (a *App) InferenceAPIKey(name string) (string, error) {
	c, err := a.current()
	if err != nil {
		return "", err
	}
	ctx, cancel := a.call()
	defer cancel()
	s := &corev1.Secret{}
	if err := c.k8s.Get(ctx, types.NamespacedName{Namespace: c.namespace(), Name: naming.InferenceAPIKeySecret(name)}, s); err != nil {
		return "", err
	}
	key := string(s.Data[naming.InferenceAPIKey])
	if key == "" {
		return "", fmt.Errorf("inference service %s has no API key yet", name)
	}
	return key, nil
}

// mutateInferenceService reads, changes and writes back an InferenceService,
// retrying when another writer changed it in between.
func (a *App) mutateInferenceService(name string, fn func(*v1.InferenceService)) (*InferenceService, error) {
	var out *InferenceService
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		c, err := a.current()
		if err != nil {
			return err
		}
		ctx, cancel := a.call()
		defer cancel()
		svc, err := getInferenceService(ctx, c, name)
		if err != nil {
			return err
		}
		fn(svc)
		if err := c.k8s.Update(ctx, svc); err != nil {
			return err
		}
		out = toInferenceService(svc)
		return nil
	})
	return out, err
}

func getInferenceService(ctx context.Context, c *Connection, name string) (*v1.InferenceService, error) {
	svc := &v1.InferenceService{}
	if err := c.k8s.Get(ctx, types.NamespacedName{Namespace: c.namespace(), Name: name}, svc); err != nil {
		return nil, err
	}
	return svc, nil
}

// normalizeInferenceForm trims the text fields and splits the extra arguments
// on white space, so "--max-model-len 65536" becomes the two arguments vLLM
// expects. Quoting is not supported.
func normalizeInferenceForm(f InferenceForm) InferenceForm {
	f.Name = strings.TrimSpace(f.Name)
	f.Model = strings.TrimSpace(f.Model)
	f.ServedName = strings.TrimSpace(f.ServedName)
	f.InstanceType = strings.TrimSpace(f.InstanceType)
	f.HuggingFaceSecret = strings.TrimSpace(f.HuggingFaceSecret)
	args := make([]string, 0, len(f.Args))
	for _, line := range f.Args {
		args = append(args, strings.Fields(line)...)
	}
	f.Args = args
	if f.Replicas == 0 {
		f.Replicas = 1
	}
	return f
}

// checkInferenceForm refuses a form the CRD or the endpoint host name would
// reject, so the user reads the reason in the form rather than a later status.
func checkInferenceForm(f InferenceForm, namespace string) error {
	if errs := validation.IsDNS1035Label(f.Name); len(errs) > 0 {
		return fmt.Errorf("name %q: %s", f.Name, strings.Join(errs, "; "))
	}
	// The endpoint host is <name>-<namespace>, a single DNS label.
	if host := f.Name + "-" + namespace; len(host) > validation.DNS1035LabelMaxLength {
		return fmt.Errorf("name %q is too long for the endpoint host %q", f.Name, host)
	}
	if !modelPattern.MatchString(f.Model) {
		return fmt.Errorf("model %q must be a Hugging Face repository, e.g. Qwen/Qwen3.8-27B-FP8", f.Model)
	}
	if len(f.ServedName) > 200 {
		return fmt.Errorf("served model name is longer than 200 characters")
	}
	if f.InstanceType == "" {
		return fmt.Errorf("choose an instance type")
	}
	if f.Replicas < 1 {
		return fmt.Errorf("replicas must be at least 1")
	}
	if f.HuggingFaceSecret != "" {
		if errs := validation.IsDNS1123Subdomain(f.HuggingFaceSecret); len(errs) > 0 {
			return fmt.Errorf("huggingFaceSecret %q: %s", f.HuggingFaceSecret, strings.Join(errs, "; "))
		}
	}
	return nil
}

// renderInferenceService builds the InferenceService a checked form describes.
func renderInferenceService(f InferenceForm, namespace string) *v1.InferenceService {
	return &v1.InferenceService{
		ObjectMeta: metav1.ObjectMeta{Name: f.Name, Namespace: namespace},
		Spec: v1.InferenceServiceSpec{
			Model:             f.Model,
			ServedModelName:   f.ServedName,
			InstanceType:      f.InstanceType,
			Replicas:          f.Replicas,
			Args:              f.Args,
			HuggingFaceSecret: f.HuggingFaceSecret,
		},
	}
}

func toInferenceService(svc *v1.InferenceService) *InferenceService {
	out := &InferenceService{
		Name:              svc.Name,
		Model:             svc.Spec.Model,
		ServedName:        inference.ServedName(svc),
		InstanceType:      svc.Spec.InstanceType,
		Replicas:          max(svc.Spec.Replicas, 1),
		ReadyReplicas:     svc.Status.ReadyReplicas,
		Endpoint:          svc.Status.Endpoint,
		APIKeySecret:      svc.Status.APIKeySecret,
		Args:              svc.Spec.Args,
		HuggingFaceSecret: svc.Spec.HuggingFaceSecret,
		Stopped:           svc.Annotations[naming.AnnotationStopped] == naming.LiteralTrue,
		Created:           svc.CreationTimestamp.UTC().Format(time.RFC3339),
		Age:               shortDur(time.Since(svc.CreationTimestamp.Time)),
	}
	if c := meta.FindStatusCondition(svc.Status.Conditions, "Ready"); c != nil {
		out.Reason = c.Reason
		out.Message = c.Message
		out.Ready = c.Status == metav1.ConditionTrue
	}
	return out
}

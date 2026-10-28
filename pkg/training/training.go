// Package training renders TrainJobs and job templates (TrainingRuntimes)
// from a job form, derives job phases and classifies failures. It is shared
// by the GUI and the operator and only works on Trainer v2 objects.
package training

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
	"github.com/wutz/forgeai/pkg/platform"
	projectv1 "github.com/wutz/workspace/api/v1alpha1"
	"github.com/wutz/workspace/pkg/instancetype"
)

// AnnotationTemplateSpec stores the job form of a template (JSON).
const AnnotationTemplateSpec = "forgeai.wutz.dev/template-spec"

// MaxNameLength leaves room for retry suffixes (-r99) within 63 characters.
const MaxNameLength = 50

// RetryOn selects which failures are retried.
type RetryOn string

const (
	// RetryInfra retries node, eviction and GPU failures (default).
	RetryInfra RetryOn = "Infra"
	// RetryAny also retries failures of the training code.
	RetryAny RetryOn = "Any"
)

// EnvVar is a name/value pair.
type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// VolumeMount mounts a project volume (PVC) into every worker.
type VolumeMount struct {
	Volume    string `json:"volume"`
	MountPath string `json:"mountPath"`
	SubPath   string `json:"subPath,omitempty"`
	ReadOnly  bool   `json:"readOnly,omitempty"`
}

// Form is a training job (or template) as entered in the GUI.
type Form struct {
	Name string `json:"name,omitempty"`
	// Template is the TrainingRuntime (job template) the job is based on.
	Template string `json:"template,omitempty"`
	// Description of a template.
	Description string `json:"description,omitempty"`
	Image       string `json:"image"`
	// Command is a bash script run on every worker (empty uses the image entrypoint).
	Command      string        `json:"command,omitempty"`
	Env          []EnvVar      `json:"env,omitempty"`
	InstanceType string        `json:"instanceType"`
	Workers      int32         `json:"workers"`
	Volumes      []VolumeMount `json:"volumes,omitempty"`
	MaxRetries   int32         `json:"maxRetries,omitempty"`
	RetryOn      RetryOn       `json:"retryOn,omitempty"`
}

var nameRE = regexp.MustCompile(`^[a-z]([-a-z0-9]*[a-z0-9])?$`)

// envNamePattern is the name a container environment variable can take.
var envNamePattern = regexp.MustCompile(`^[-._a-zA-Z][-._a-zA-Z0-9]*$`)

// reservedMountPath is the shared memory the job mounts itself (see volumes).
const reservedMountPath = "/dev/shm"

// Validate checks a form; it does not look up the instance type or volumes.
func (f *Form) Validate() error {
	if !nameRE.MatchString(f.Name) || len(f.Name) > MaxNameLength {
		return fmt.Errorf("invalid name %q: lowercase letters, digits and '-', starting with a letter, at most %d characters",
			f.Name, MaxNameLength)
	}
	if strings.TrimSpace(f.Image) == "" {
		return fmt.Errorf("image is required")
	}
	if f.InstanceType == "" {
		return fmt.Errorf("instance type is required")
	}
	if f.Workers < 1 {
		return fmt.Errorf("workers must be at least 1")
	}
	if f.MaxRetries < 0 || f.MaxRetries > 20 {
		return fmt.Errorf("maxRetries must be between 0 and 20")
	}
	if f.RetryOn != "" && f.RetryOn != RetryInfra && f.RetryOn != RetryAny {
		return fmt.Errorf("retryOn must be Infra or Any")
	}
	seen := map[string]bool{}
	for _, e := range f.Env {
		if !envNamePattern.MatchString(e.Name) || seen[e.Name] {
			return fmt.Errorf("environment variable names must be unique and valid")
		}
		seen[e.Name] = true
	}
	paths := map[string]bool{}
	for _, m := range f.Volumes {
		if m.Volume == "" || !strings.HasPrefix(m.MountPath, "/") || paths[m.MountPath] {
			return fmt.Errorf("volume mounts need a volume and a unique absolute mount path")
		}
		if m.MountPath == reservedMountPath {
			return fmt.Errorf("%s is mounted by the job itself", reservedMountPath)
		}
		if m.SubPath != "" && (strings.HasPrefix(m.SubPath, "/") || slices.Contains(strings.Split(m.SubPath, "/"), "..")) {
			return fmt.Errorf("sub path must be relative and stay inside the volume")
		}
		paths[m.MountPath] = true
	}
	return nil
}

// Context is what a job needs beyond the form.
type Context struct {
	Namespace    string
	InstanceType *projectv1.InstanceType
	Config       *platform.Config
	// RegistrySecret is added to imagePullSecrets when set.
	RegistrySecret string
}

func command(script string) []string {
	if strings.TrimSpace(script) == "" {
		return nil
	}
	return []string{"/bin/bash", "-c", script}
}

func env(in []EnvVar) []corev1.EnvVar {
	out := make([]corev1.EnvVar, 0, len(in))
	for _, e := range in {
		out = append(out, corev1.EnvVar{Name: e.Name, Value: e.Value})
	}
	return out
}

// Resources returns the per-worker requests and limits of an instance type.
func Resources(it *projectv1.InstanceType) corev1.ResourceRequirements {
	rl := corev1.ResourceList{
		corev1.ResourceCPU:       it.Spec.CPU.DeepCopy(),
		corev1.ResourceMemory:    it.Spec.Memory.DeepCopy(),
		instancetype.ResourceGPU: *resource.NewQuantity(int64(it.Spec.GPU.Count), resource.DecimalSI),
	}
	return corev1.ResourceRequirements{Requests: rl, Limits: rl.DeepCopy()}
}

func volumes(f *Form, shm resource.Quantity) ([]corev1.Volume, []corev1.VolumeMount) {
	vols := []corev1.Volume{{Name: "dshm", VolumeSource: corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: &shm},
	}}}
	mounts := make([]corev1.VolumeMount, 1, 1+len(f.Volumes))
	mounts[0] = corev1.VolumeMount{Name: "dshm", MountPath: reservedMountPath}
	seen := map[string]bool{}
	for _, m := range f.Volumes {
		name := "vol-" + m.Volume
		if !seen[m.Volume] {
			seen[m.Volume] = true
			vols = append(vols, corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: m.Volume},
			}})
		}
		mounts = append(mounts, corev1.VolumeMount{
			Name: name, MountPath: m.MountPath, SubPath: m.SubPath, ReadOnly: m.ReadOnly,
		})
	}
	return vols, mounts
}

// TrainJob renders a TrainJob from a form. The runtime is the template, or
// the platform default ClusterTrainingRuntime. Image, command, env, nodes and
// resources are trainer overrides; placement, volumes and the pull secret
// are a runtime patch owned by forgeai.
func TrainJob(f *Form, c Context) (*kf.TrainJob, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	it := c.InstanceType
	if it == nil || it.Name != f.InstanceType {
		return nil, fmt.Errorf("instance type %q not found", f.InstanceType)
	}
	ref := kf.RuntimeRef{
		APIGroup: kf.Group, Kind: kf.ClusterTrainingRuntimeGVK.Kind, Name: c.Config.Training.DefaultRuntime,
	}
	if f.Template != "" {
		ref = kf.RuntimeRef{APIGroup: kf.Group, Kind: kf.TrainingRuntimeGVK.Kind, Name: f.Template}
	}
	res := Resources(it)
	vols, mounts := volumes(f, c.Config.Training.ShmSize)
	pod := &kf.PodSpecPatch{
		NodeSelector: it.Spec.NodeSelector,
		Tolerations: []corev1.Toleration{{
			Key: string(instancetype.ResourceGPU), Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule,
		}},
		Volumes:    vols,
		Containers: []kf.ContainerPatch{{Name: kf.ContainerNode, VolumeMounts: mounts}},
	}
	if c.RegistrySecret != "" {
		pod.ImagePullSecrets = []corev1.LocalObjectReference{{Name: c.RegistrySecret}}
	}
	retryOn := f.RetryOn
	if retryOn == "" {
		retryOn = RetryInfra
	}
	tj := &kf.TrainJob{
		TypeMeta: metav1.TypeMeta{APIVersion: kf.TrainJobGVK.GroupVersion().String(), Kind: kf.TrainJobGVK.Kind},
		ObjectMeta: metav1.ObjectMeta{
			Name: f.Name, Namespace: c.Namespace,
			Labels: map[string]string{
				naming.LabelManagedBy:    naming.ManagedBy,
				kf.LabelKueueQueue:       naming.LocalQueueName,
				naming.LabelInstanceType: it.Name,
				naming.LabelJobGroup:     f.Name,
			},
			Annotations: map[string]string{
				naming.AnnotationMaxRetries: fmt.Sprint(f.MaxRetries),
				naming.AnnotationRetryOn:    string(retryOn),
			},
		},
		Spec: kf.TrainJobSpec{
			RuntimeRef: ref,
			Trainer: &kf.Trainer{
				Image:            f.Image,
				Command:          command(f.Command),
				Env:              env(f.Env),
				NumNodes:         new(f.Workers),
				NumProcPerNode:   new(it.Spec.GPU.Count),
				ResourcesPerNode: &res,
			},
			RuntimePatches: []kf.RuntimePatch{{
				Manager: kf.PatchManager,
				TrainingRuntimeSpec: &kf.TrainingRuntimeSpecPatch{Template: &kf.JobSetTemplatePatch{Spec: &kf.JobSetSpecPatch{
					ReplicatedJobs: []kf.ReplicatedJobPatch{{
						Name: kf.ContainerNode,
						Template: &kf.JobTemplatePatch{Spec: &kf.JobSpecPatch{Template: &kf.PodTemplatePatch{
							Metadata: &metav1.ObjectMeta{Labels: map[string]string{naming.LabelJobGroup: f.Name}},
							Spec:     pod,
						}}},
					}},
				}}},
			}},
		},
	}
	if f.Template != "" {
		tj.Labels[naming.LabelTemplate] = f.Template
	}
	return tj, nil
}

// Template renders a job template as a TrainingRuntime. The runtime is a
// plain torch runtime with the image and command, usable with kubectl; the
// full form is kept in an annotation to prefill the GUI.
func Template(f *Form, namespace string) (*kf.TrainingRuntime, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	spec := *f
	spec.Name, spec.Template = "", ""
	data, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	cmd := command(f.Command)
	return &kf.TrainingRuntime{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kf.TrainingRuntimeGVK.GroupVersion().String(), Kind: kf.TrainingRuntimeGVK.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: f.Name, Namespace: namespace,
			Labels: map[string]string{
				naming.LabelManagedBy: naming.ManagedBy,
				naming.LabelTemplate:  naming.LiteralTrue,
				kf.LabelFramework:     "torch",
			},
			Annotations: map[string]string{AnnotationTemplateSpec: string(data), naming.AnnotationDescription: f.Description},
		},
		Spec: kf.TrainingRuntimeSpec{
			MLPolicy: &kf.MLPolicy{NumNodes: new(f.Workers), Torch: &struct{}{}},
			Template: kf.JobSetTemplateSpec{Spec: kf.JobSetSpec{ReplicatedJobs: []kf.ReplicatedJob{{
				Name: kf.ContainerNode, Replicas: 1,
				Template: batchJobTemplate(f.Image, cmd, env(f.Env)),
			}}}},
		},
	}, nil
}

// TemplateForm returns the job form stored in a template.
func TemplateForm(rt *kf.TrainingRuntime) (*Form, error) {
	f := &Form{}
	if s := rt.Annotations[AnnotationTemplateSpec]; s != "" {
		if err := json.Unmarshal([]byte(s), f); err != nil {
			return nil, fmt.Errorf("template %s: %w", rt.Name, err)
		}
	}
	f.Name, f.Template = "", rt.Name
	if f.Description == "" {
		f.Description = rt.Annotations[naming.AnnotationDescription]
	}
	if f.Image == "" {
		// A runtime created outside forgeai: take the trainer container.
		for _, rj := range rt.Spec.Template.Spec.ReplicatedJobs {
			for _, ct := range rj.Template.Spec.Template.Spec.Containers {
				if ct.Name == kf.ContainerNode {
					f.Image = ct.Image
				}
			}
		}
	}
	if f.Workers < 1 {
		f.Workers = 1
		if p := rt.Spec.MLPolicy; p != nil && p.NumNodes != nil {
			f.Workers = *p.NumNodes
		}
	}
	return f, nil
}

// SortedEnv returns env vars sorted by name.
func SortedEnv(m map[string]string) []EnvVar {
	out := make([]EnvVar, 0, len(m))
	for k, v := range m {
		out = append(out, EnvVar{Name: k, Value: v})
	}
	slices.SortFunc(out, func(a, b EnvVar) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// TrainJobForm extracts the job form of a TrainJob rendered by TrainJob:
// the trainer overrides, the instance type label, the volumes from the
// forgeai runtime patch and the retry annotations.
func TrainJobForm(tj *kf.TrainJob) (*Form, error) {
	f := &Form{
		Name:         tj.Name,
		InstanceType: tj.Labels[naming.LabelInstanceType],
		Template:     tj.Labels[naming.LabelTemplate],
	}
	if n, err := strconv.ParseInt(tj.Annotations[naming.AnnotationMaxRetries], 10, 32); err == nil {
		f.MaxRetries = int32(n)
	}
	f.RetryOn = RetryOn(tj.Annotations[naming.AnnotationRetryOn])
	if t := tj.Spec.Trainer; t != nil {
		f.Image = t.Image
		if len(t.Command) >= 3 && t.Command[0] == "/bin/bash" && t.Command[1] == "-c" {
			f.Command = strings.Join(t.Command[2:], " ")
		} else {
			f.Command = strings.Join(t.Command, " ")
		}
		for _, e := range t.Env {
			f.Env = append(f.Env, EnvVar{Name: e.Name, Value: e.Value})
		}
		if t.NumNodes != nil {
			f.Workers = *t.NumNodes
		}
	}
	for _, p := range tj.Spec.RuntimePatches {
		if p.Manager != kf.PatchManager || p.TrainingRuntimeSpec == nil || p.TrainingRuntimeSpec.Template == nil {
			continue
		}
		for _, rj := range p.TrainingRuntimeSpec.Template.Spec.ReplicatedJobs {
			if rj.Template == nil || rj.Template.Spec == nil || rj.Template.Spec.Template == nil {
				continue
			}
			spec := rj.Template.Spec.Template.Spec
			for i := range spec.Volumes {
				v := &spec.Volumes[i]
				if v.PersistentVolumeClaim == nil {
					continue
				}
				for _, ctr := range spec.Containers {
					for _, m := range ctr.VolumeMounts {
						if m.Name != v.Name {
							continue
						}
						f.Volumes = append(f.Volumes, VolumeMount{
							Volume: v.PersistentVolumeClaim.ClaimName, MountPath: m.MountPath,
							SubPath: m.SubPath, ReadOnly: m.ReadOnly,
						})
					}
				}
			}
		}
	}
	if f.Workers < 1 {
		f.Workers = 1
	}
	return f, nil
}

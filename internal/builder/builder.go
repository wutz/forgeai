// Package builder renders the child objects of a TrainingJob. It is a pure
// function of the TrainingJob spec and the platform config, so it is easy to test.
package builder

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	v1 "github.com/scclabs/forgeai/api/v1alpha1"
	"github.com/scclabs/forgeai/pkg/kf"
	"github.com/scclabs/forgeai/pkg/naming"
	"github.com/scclabs/forgeai/pkg/platform"
)

const containerName = "node"

// Builder renders objects for one TrainingJob.
type Builder struct {
	Job      *v1.TrainingJob
	Config   *platform.Config
	Resource platform.Resource
}

// New resolves the resource model and returns a Builder.
func New(job *v1.TrainingJob, cfg *platform.Config) (*Builder, error) {
	res, err := cfg.Resource(job.Spec.ResourceID)
	if err != nil {
		return nil, err
	}
	return &Builder{Job: job, Config: cfg, Resource: res}, nil
}

func (b *Builder) workers() int32 {
	if b.Job.Spec.Workers < 1 {
		return 1
	}
	return b.Job.Spec.Workers
}

func (b *Builder) multiNode() bool { return b.workers() > 1 }

// GPUs per worker. Multi-node jobs may be forced to full nodes.
func (b *Builder) GPUs() int {
	gpus := b.Resource.GPUCount()
	if b.multiNode() && gpus > 0 && b.Config.MultiNode.FullNodeGPUs > 0 {
		gpus = b.Config.MultiNode.FullNodeGPUs
	}
	return gpus
}

// SelectorLabels identify the pods of this job.
func (b *Builder) SelectorLabels() map[string]string {
	return map[string]string{
		naming.LabelAppName:     naming.AppNameTrainingJob,
		naming.LabelPodName:     b.Job.Name,
		naming.LabelUserID:      b.Job.Spec.UserID,
		naming.LabelPodID:       b.Job.Spec.JobID,
		naming.LabelTrainingJob: b.Job.Name,
	}
}

// Labels are set on every child object.
func (b *Builder) Labels() map[string]string {
	l := b.SelectorLabels()
	l[naming.LabelTrainerJobID] = b.Job.Spec.JobID
	l[naming.LabelAppInstance] = b.Job.Name
	l[naming.LabelResourceID] = b.Job.Spec.ResourceID
	l[naming.LabelAppVersion] = b.Config.ReleaseVersion
	l[naming.LabelManagedBy] = naming.ManagedBy
	l[naming.LabelComponent] = naming.ComponentTraining
	if b.Job.Spec.UserGroup != "" {
		l[naming.LabelUserGroup] = b.Job.Spec.UserGroup
	}
	return l
}

func (b *Builder) meta(name string, labels map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: b.Job.Namespace, Labels: labels}
}

func (b *Builder) rdma() (platform.RDMAProfile, bool) {
	if !b.multiNode() || b.Resource.GPUCount() == 0 {
		return platform.RDMAProfile{}, false
	}
	if p, ok := b.Config.MultiNode.RDMA[b.Resource.Labels[naming.LabelGPU]]; ok {
		return p, true
	}
	p, ok := b.Config.MultiNode.RDMA["*"]
	return p, ok
}

func (b *Builder) resources() (corev1.ResourceRequirements, error) {
	rl := corev1.ResourceList{}
	cpu, err := resource.ParseQuantity(fmt.Sprint(b.Resource.CPU.Cores))
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("cpu: %w", err)
	}
	rl[corev1.ResourceCPU] = cpu
	mem, err := resource.ParseQuantity(fmt.Sprintf("%vGi", b.Resource.Memory.SizeGB))
	if err != nil {
		return corev1.ResourceRequirements{}, fmt.Errorf("memory: %w", err)
	}
	rl[corev1.ResourceMemory] = mem
	extra := map[string]string{"ephemeral-storage": b.Config.EphemeralStorage}
	maps.Copy(extra, b.Config.ExtraResources)
	if p, ok := b.rdma(); ok {
		maps.Copy(extra, p.Resources)
	}
	if g := b.GPUs(); g > 0 {
		extra["nvidia.com/gpu"] = fmt.Sprint(g)
	}
	for k, v := range extra {
		q, err := resource.ParseQuantity(v)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("resource %s: %w", k, err)
		}
		rl[corev1.ResourceName(k)] = q
	}
	return corev1.ResourceRequirements{Requests: rl, Limits: rl.DeepCopy()}, nil
}

func (b *Builder) env() []corev1.EnvVar {
	var env []corev1.EnvVar
	if b.multiNode() {
		keys := make([]string, 0, len(b.Config.MultiNode.Env))
		for k := range b.Config.MultiNode.Env {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			env = append(env, corev1.EnvVar{Name: k, Value: b.Config.MultiNode.Env[k]})
		}
	}
	for _, e := range b.Job.Spec.Env {
		env = append(env, corev1.EnvVar{Name: e.Name, Value: e.Value})
	}
	return env
}

func (b *Builder) volumes() ([]corev1.Volume, []corev1.VolumeMount) {
	var vols []corev1.Volume
	mounts := make([]corev1.VolumeMount, 0, len(b.Job.Spec.VolumeMounts)+1)
	seen := map[string]bool{}
	for _, m := range b.Job.Spec.VolumeMounts {
		if !seen[m.VolumeID] {
			seen[m.VolumeID] = true
			claim := m.ClaimName
			if claim == "" {
				owner := m.UserID
				if owner == "" {
					owner = b.Job.Spec.UserID
				}
				claim = naming.VolumeClaimName(b.Job.Namespace, owner, m.VolumeID)
			}
			vols = append(vols, corev1.Volume{Name: m.VolumeID, VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim},
			}})
		}
		mounts = append(mounts, corev1.VolumeMount{
			Name: m.VolumeID, MountPath: m.MountPath, SubPath: m.SubPath, ReadOnly: m.ReadOnly,
		})
	}
	vols = append(vols, corev1.Volume{Name: "dshm", VolumeSource: corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory},
	}})
	mounts = append(mounts, corev1.VolumeMount{Name: "dshm", MountPath: "/dev/shm"})
	return vols, mounts
}

func (b *Builder) command() ([]string, []string) {
	if b.Job.Spec.Command == "" {
		return nil, nil
	}
	return []string{"/bin/bash", "-c"}, []string{b.Job.Spec.Command}
}

// TrainingRuntime renders the per-job runtime.
func (b *Builder) TrainingRuntime() (*kf.TrainingRuntime, error) {
	res, err := b.resources()
	if err != nil {
		return nil, err
	}
	vols, mounts := b.volumes()
	cmd, args := b.command()

	nodeSelector := map[string]string{}
	maps.Copy(nodeSelector, b.Resource.Labels)

	labels := b.Labels()
	podAnno := map[string]string{}
	var affinity *corev1.Affinity
	if b.multiNode() {
		if p, ok := b.rdma(); ok && len(p.Networks) > 0 {
			nets, _ := json.Marshal(p.Networks)
			podAnno[kf.AnnotationNetworks] = string(nets)
		}
		maps.Copy(podAnno, b.Config.MultiNode.PodSetTopology)
		if key := b.Config.MultiNode.LeafTopologyKey; key != "" {
			affinity = &corev1.Affinity{PodAffinity: &corev1.PodAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
					Weight: 100,
					PodAffinityTerm: corev1.PodAffinityTerm{
						TopologyKey:   key,
						LabelSelector: &metav1.LabelSelector{MatchLabels: b.SelectorLabels()},
					},
				}},
			}}
		}
	}

	pod := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: podAnno},
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{FSGroup: ptr.To[int64](1000)},
			Containers: []corev1.Container{{
				Name:    containerName,
				Image:   b.Job.Spec.Image,
				Command: cmd,
				Args:    args,
				Env:     b.env(),
				// No extra capabilities: the pod network is shared between users.
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: new(false),
				},
				Resources:    res,
				VolumeMounts: mounts,
			}},
			Affinity:                      affinity,
			NodeSelector:                  nodeSelector,
			EnableServiceLinks:            new(false),
			ServiceAccountName:            b.Config.ServiceAccount,
			Volumes:                       vols,
			Tolerations:                   []corev1.Toleration{{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule}},
			TerminationGracePeriodSeconds: ptr.To[int64](60),
			RestartPolicy:                 corev1.RestartPolicyNever,
		},
	}

	jobLabels := b.Labels()
	jobLabels[kf.LabelAncestorStep] = kf.AncestorTrainer

	rtLabels := b.Labels()
	rtLabels[kf.LabelFramework] = "torch"

	return &kf.TrainingRuntime{
		TypeMeta:   metav1.TypeMeta{APIVersion: kf.TrainingRuntimeGVK.GroupVersion().String(), Kind: kf.TrainingRuntimeGVK.Kind},
		ObjectMeta: b.meta(naming.RuntimeName(b.Job.Name), rtLabels),
		Spec: kf.TrainingRuntimeSpec{
			MLPolicy: &kf.MLPolicy{NumNodes: new(b.workers()), Torch: &struct{}{}},
			Template: kf.JobSetTemplateSpec{Spec: kf.JobSetSpec{
				ReplicatedJobs: []kf.ReplicatedJob{{
					Name:     containerName,
					Replicas: 1,
					Template: batchv1.JobTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: jobLabels},
						Spec: batchv1.JobSpec{
							CompletionMode: ptr.To(batchv1.IndexedCompletion),
							BackoffLimit:   ptr.To[int32](0),
							Template:       pod,
						},
					},
				}},
				TTLSecondsAfterFinished: ptr.To[int32](86400),
			}},
		},
	}, nil
}

// TrainJob renders the Kubeflow TrainJob. Kueue manages it via the queue label.
func (b *Builder) TrainJob() *kf.TrainJob {
	labels := b.Labels()
	queue := b.Job.Spec.QueueName
	if queue == "" {
		queue = b.Config.DefaultQueue
	}
	labels[kf.LabelKueueQueue] = queue
	var trainer *kf.Trainer
	if g := b.GPUs(); g > 0 {
		trainer = &kf.Trainer{NumProcPerNode: new(int32(g))}
	}
	m := b.meta(b.Job.Name, labels)
	m.Annotations = map[string]string{naming.AnnotationRestartGeneration: fmt.Sprint(b.Job.Spec.RestartGeneration)}
	return &kf.TrainJob{
		TypeMeta:   metav1.TypeMeta{APIVersion: kf.TrainJobGVK.GroupVersion().String(), Kind: kf.TrainJobGVK.Kind},
		ObjectMeta: m,
		Spec: kf.TrainJobSpec{
			RuntimeRef: kf.RuntimeRef{APIGroup: kf.TrainingRuntimeGVK.Group, Kind: kf.TrainingRuntimeGVK.Kind, Name: naming.RuntimeName(b.Job.Name)},
			Trainer:    trainer,
		},
	}
}

// UserNetworkPolicy renders the scc-k8s-v2 style per-user egress policy: user pods
// may only talk to pods of the same user (in addition to other policies).
func UserNetworkPolicy(namespace, userID string) *networkingv1.NetworkPolicy {
	sel := metav1.LabelSelector{MatchLabels: map[string]string{naming.LabelUserID: userID}}
	return &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
		ObjectMeta: metav1.ObjectMeta{
			Name: naming.NetworkPolicyName(namespace, userID), Namespace: namespace,
			Labels: map[string]string{naming.LabelManagedBy: naming.ManagedBy, naming.LabelUserID: userID},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: sel,
			Egress:      []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{PodSelector: &sel}}}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
		},
	}
}

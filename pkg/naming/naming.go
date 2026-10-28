// Package naming holds object names, label keys and annotations shared by the
// operator and the GUI.
package naming

import (
	"regexp"
	"strings"
)

// Labels and annotations.
const (
	LabelManagedBy = "app.kubernetes.io/managed-by"
	LabelComponent = "app.kubernetes.io/component"
	ManagedBy      = "forgeai"

	// LabelProject is set on project namespaces and every object forgeai
	// creates for a project (value = Project name).
	LabelProject = "workspace.wutz.dev/project"
	// LabelMember is set on member ServiceAccounts and token Secrets.
	LabelMember = "workspace.wutz.dev/member"

	// LabelFileServerIgnore marks a volume (PVC) in the trash. The fileserver
	// operator does not serve a volume with it, so forgeai sets it when it
	// trashes one.
	LabelFileServerIgnore = "fileserver.wutz.dev/ignore"
	// LabelTrashLegacy is the trash label before fileserver 0.3.0. Volumes that
	// still carry it are moved to LabelFileServerIgnore.
	LabelTrashLegacy = "forgeai.wutz.dev/trash"
	// AnnotationTrashedAt is when the volume was moved to the trash (RFC3339).
	AnnotationTrashedAt = "forgeai.wutz.dev/trashed-at"
	// AnnotationExpireAt is when a trashed volume is purged (RFC3339).
	AnnotationExpireAt = "forgeai.wutz.dev/expire-at"
	// LabelVolumeType is the platform storage type of a volume.
	LabelVolumeType = "forgeai.wutz.dev/volume-type"

	// AnnotationLastActive is a client heartbeat on a FileServer (RFC3339).
	AnnotationLastActive = "forgeai.wutz.dev/last-active"
	// AnnotationStopped marks a TrainJob or Sandbox the user stopped.
	AnnotationStopped = "forgeai.wutz.dev/stopped"
	// AnnotationIdleSince is when the operator suspended an idle Sandbox.
	AnnotationIdleSince = "forgeai.wutz.dev/idle-since"
	// AnnotationIdleTimeout is the idle timeout a Sandbox was created with
	// (Go duration); absent means the platform's sandbox.idleTimeout applies.
	AnnotationIdleTimeout = "forgeai.wutz.dev/idle-timeout"
	// AnnotationGatewayPort is the port a Sandbox serves HTTP on, published
	// through the platform gateway; absent means it is not published.
	AnnotationGatewayPort = "forgeai.wutz.dev/gateway-port"

	// LabelTemplate marks a TrainingRuntime as a user job template.
	LabelTemplate = "forgeai.wutz.dev/template"
	// AnnotationDescription describes a template.
	AnnotationDescription = "forgeai.wutz.dev/description"
	// LabelInstanceType is the InstanceType of a TrainJob.
	LabelInstanceType = "forgeai.wutz.dev/instance-type"
	// LabelJobGroup groups a TrainJob with its retries (value = first job name).
	LabelJobGroup = "forgeai.wutz.dev/job-group"
	// AnnotationMaxRetries is how often a failed TrainJob is resubmitted.
	AnnotationMaxRetries = "forgeai.wutz.dev/max-retries"
	// AnnotationRetryOn is Infra (default) or Any.
	AnnotationRetryOn = "forgeai.wutz.dev/retry-on"
	// AnnotationRetry is the attempt number of a resubmitted TrainJob.
	AnnotationRetry = "forgeai.wutz.dev/retry"
	// AnnotationFailureReason is the failure classification of a TrainJob.
	AnnotationFailureReason = "forgeai.wutz.dev/failure-reason"
	// AnnotationFailureMessage details the failure.
	AnnotationFailureMessage = "forgeai.wutz.dev/failure-message"
	// AnnotationObservedFailureReason is the worker failure seen while the
	// TrainJob ran. The Trainer removes its pods once the job fails, so this
	// is the record of the cause when the failure is classified afterwards.
	AnnotationObservedFailureReason = "forgeai.wutz.dev/observed-failure-reason"
	// AnnotationObservedFailureMessage details the observed worker failure.
	AnnotationObservedFailureMessage = "forgeai.wutz.dev/observed-failure-message"
	// AnnotationRetriedAs is the name of the TrainJob that retried this one.
	AnnotationRetriedAs = "forgeai.wutz.dev/retried-as"

	// LabelGPUClass is set on generated InstanceTypes and ResourceFlavors.
	LabelGPUClass = "workspace.wutz.dev/gpu-class"

	// Node labels from NVIDIA GPU feature discovery.
	LabelGPUProduct = "nvidia.com/gpu.product"
	LabelGPUCount   = "nvidia.com/gpu.count"
	LabelGPUMemory  = "nvidia.com/gpu.memory"

	// LabelInference is set on the model servers and the llm-d router of an
	// InferenceService (value = InferenceService name). The router selects its
	// model servers by it.
	LabelInference = "forgeai.wutz.dev/inference"

	// LabelSSH is set on a sandbox that serves SSH (value = "true"). The
	// operator adds the sshd sidecar and the Service of the sandbox for it.
	LabelSSH = "forgeai.wutz.dev/ssh"
	// LabelSandbox is set on the pods of a sandbox (value = Sandbox name). The
	// SSH Service selects its sshd by it.
	LabelSandbox = "forgeai.wutz.dev/sandbox"

	ComponentFileServer = "fileserver"
	ComponentSandbox    = "sandbox"
	ComponentInference  = "inference"
	ComponentSSHGateway = "ssh-gateway"
)

// SSHSidecarContainer is the name of the sshd container of a sandbox.
const SSHSidecarContainer = "sshd"

// SSHIdentitySecretName is the platform Secret in the platform namespace that
// holds the key the gateway uses to log in to sandboxes.
const SSHIdentitySecretName = "sshgateway-identity"

// SSHHostKeySecret is the Secret of an SSH Service that holds the host key of
// the sshd behind it. The gateway pins that key from it.
func SSHHostKeySecret(service string) string { return service + "-hostkey" }

// SSHServiceName is the Service that exposes the sshd of a sandbox.
func SSHServiceName(sandbox string) string { return sandbox + "-ssh" }

// SSHGatewayName is the name of the sshpiper Deployment, Service and
// ConfigMap of an SSHGateway.
func SSHGatewayName(gateway string) string { return "sshgateway-" + gateway }

// InferenceAPIKey is the key of the API key Secret of an InferenceService.
const InferenceAPIKey = "api-key"

// InferenceAPIKeySecret is the Secret holding the API key of an InferenceService.
func InferenceAPIKeySecret(service string) string { return service + "-api-key" }

// Fixed names inside a project namespace.
const (
	LocalQueueName     = "default"
	RegistrySecretName = "forgeai-registry"
	NetworkPolicyName  = "forgeai-isolation"
	ResourceQuotaName  = "forgeai-quota"
	LimitRangeName     = "forgeai-defaults"
)

// Namespace is the namespace of a project.
func Namespace(prefix, project string) string { return prefix + project }

// ClusterQueue is the Kueue ClusterQueue of a project.
func ClusterQueue(namespace string) string { return namespace }

// MemberRoleBinding binds a member to its role in the project namespace.
func MemberRoleBinding(member string) string { return "forgeai-member-" + member }

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// GPUClass normalises a nvidia.com/gpu.product value (NVIDIA-GeForce-RTX-5090)
// to a short DNS label (rtx-5090).
func GPUClass(product string) string {
	s := strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(product), "-"), "-")
	for _, p := range []string{"nvidia-", "geforce-", "tesla-"} {
		s = strings.TrimPrefix(s, p)
	}
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	return s
}

// InstanceTypeName is the InstanceType of count GPUs of a class, e.g. rtx-5090.x2.
func InstanceTypeName(class string, count int32) string {
	return class + ".x" + itoa(count)
}

// ResourceFlavor is the Kueue ResourceFlavor of a GPU class.
func ResourceFlavor(class string) string { return "forgeai-" + class }

func itoa(n int32) string {
	if n == 0 {
		return "0"
	}
	var b [11]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Keys of unstructured object maps.
const (
	KeyAPIVersion = "apiVersion"
	KeyKind       = "kind"
	KeyMetadata   = "metadata"
	KeyName       = "name"
	KeyNamespace  = "namespace"
	KeyLabels     = "labels"
	KeySpec       = "spec"
	KeyStatus     = "status"
)

// LiteralTrue is the string form of a boolean true in labels and annotations.
const LiteralTrue = "true"

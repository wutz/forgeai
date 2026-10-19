package training

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wutz/forgeai/pkg/kf"
	"github.com/wutz/forgeai/pkg/naming"
)

// AnnotationStopped is set by users to stop a job; the operator deactivates
// its Kueue workload, which releases the pods and keeps the job.
const AnnotationStopped = "forgeai.wutz.dev/stopped"

// Phase is a summary of a TrainJob's lifecycle.
type Phase string

const (
	PhaseQueued    Phase = "Queued"
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseSucceeded Phase = "Succeeded"
	PhaseFailed    Phase = "Failed"
	PhaseStopped   Phase = "Stopped"
	// PhaseRetried is a failed job that has been resubmitted.
	PhaseRetried Phase = "Retried"
)

// Failure reasons written to forgeai.wutz.dev/failure-reason.
const (
	ReasonNodeFailure     = "NodeFailure"
	ReasonEvicted         = "Evicted"
	ReasonOOMKilled       = "OOMKilled"
	ReasonImagePullFailed = "ImagePullFailed"
	ReasonConfigError     = "ConfigError"
	ReasonExitCode        = "ExitCode"
	ReasonUnknown         = "Unknown"
)

// Infra reports whether a failure is caused by the platform rather than the
// job, so that rerunning the same spec may succeed.
func Infra(reason string) bool { return reason == ReasonNodeFailure || reason == ReasonEvicted }

// Retryable reports whether a failure is retried under a retry policy.
func Retryable(reason string, on RetryOn) bool {
	if on == RetryAny {
		return reason != ReasonImagePullFailed && reason != ReasonConfigError
	}
	return Infra(reason)
}

// Finished reports whether the TrainJob completed or failed.
func Finished(tj *kf.TrainJob) bool {
	c := conditions(tj)
	return meta.IsStatusConditionTrue(c, kf.ConditionComplete) || meta.IsStatusConditionTrue(c, kf.ConditionFailed)
}

// Failed reports whether the TrainJob failed, including jobs stopped by the
// operator because they were stuck.
func Failed(tj *kf.TrainJob) bool {
	return meta.IsStatusConditionTrue(conditions(tj), kf.ConditionFailed) ||
		(tj.Annotations[naming.AnnotationFailureReason] != "" && tj.Annotations[AnnotationStopped] == naming.LiteralTrue)
}

func conditions(tj *kf.TrainJob) []metav1.Condition {
	if tj.Status == nil {
		return nil
	}
	return tj.Status.Conditions
}

// JobPhase derives the phase of a TrainJob from its conditions and pods.
func JobPhase(tj *kf.TrainJob, pods []corev1.Pod) (Phase, string) {
	c := conditions(tj)
	ann := tj.Annotations
	switch {
	case ann[naming.AnnotationRetriedAs] != "" && Failed(tj):
		return PhaseRetried, "retried as " + ann[naming.AnnotationRetriedAs]
	case meta.IsStatusConditionTrue(c, kf.ConditionComplete):
		return PhaseSucceeded, ""
	case Failed(tj):
		msg := ann[naming.AnnotationFailureMessage]
		if msg == "" {
			if f := meta.FindStatusCondition(c, kf.ConditionFailed); f != nil {
				msg = f.Message
			}
		}
		return PhaseFailed, msg
	case ann[AnnotationStopped] == naming.LiteralTrue:
		return PhaseStopped, ""
	case meta.IsStatusConditionTrue(c, kf.ConditionSuspended):
		return PhaseQueued, "waiting for quota"
	}
	msg := ""
	for i := range pods {
		p := &pods[i]
		if p.Status.Phase == corev1.PodRunning && p.DeletionTimestamp.IsZero() {
			return PhaseRunning, ""
		}
		if m := waitingMessage(p); m != "" && msg == "" {
			msg = m
		}
	}
	return PhasePending, msg
}

func waitingMessage(p *corev1.Pod) string {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse {
			return c.Reason + ": " + c.Message
		}
	}
	for _, cs := range p.Status.ContainerStatuses {
		if w := cs.State.Waiting; w != nil && w.Reason != "ContainerCreating" {
			return strings.TrimSuffix(w.Reason+": "+w.Message, ": ")
		}
	}
	return ""
}

// WorkerIndex returns the node index of a training pod.
func WorkerIndex(p *corev1.Pod) int {
	i, err := strconv.Atoi(p.Labels[kf.LabelCompletion])
	if err != nil {
		return -1
	}
	return i
}

func podName(p *corev1.Pod) string {
	if i := WorkerIndex(p); i >= 0 {
		return fmt.Sprintf("worker-%d", i)
	}
	return p.Name
}

// Failure is a classified job failure.
type Failure struct {
	Reason  string
	Message string
}

func byPriority(failures []Failure) Failure {
	order := []string{ReasonNodeFailure, ReasonEvicted, ReasonOOMKilled, ReasonImagePullFailed,
		ReasonConfigError, ReasonExitCode}
	slices.SortStableFunc(failures, func(a, b Failure) int {
		ia, ib := slices.Index(order, a.Reason), slices.Index(order, b.Reason)
		if ia < 0 {
			ia = len(order)
		}
		if ib < 0 {
			ib = len(order)
		}
		return ia - ib
	})
	return failures[0]
}

// Classify explains why a job's pods failed. Platform failures win over the
// exit codes they cause on other workers. notReady lists nodes that are
// NotReady or gone.
func Classify(pods []corev1.Pod, notReady map[string]bool) Failure {
	var failures []Failure
	for i := range pods {
		p := &pods[i]
		name := podName(p)
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.DisruptionTarget && c.Status == corev1.ConditionTrue {
				reason := ReasonEvicted
				if c.Reason == "DeletionByTaintManager" {
					reason = ReasonNodeFailure
				}
				failures = append(failures, Failure{reason, fmt.Sprintf("%s: %s: %s", name, c.Reason, c.Message)})
			}
		}
		switch p.Status.Reason {
		case "Evicted", "Preempting":
			failures = append(failures, Failure{ReasonEvicted, fmt.Sprintf("%s: %s", name, p.Status.Message)})
		case "NodeLost", "UnexpectedAdmissionError", "NodeAffinity", "Terminated":
			msg := fmt.Sprintf("%s: %s %s", name, p.Status.Reason, p.Status.Message)
			failures = append(failures, Failure{ReasonNodeFailure, msg})
		}
		for _, cs := range p.Status.ContainerStatuses {
			t := cs.State.Terminated
			if t == nil {
				t = cs.LastTerminationState.Terminated
			}
			if w := cs.State.Waiting; w != nil {
				switch w.Reason {
				case "ErrImagePull", "ImagePullBackOff", "InvalidImageName":
					failures = append(failures, Failure{ReasonImagePullFailed, fmt.Sprintf("%s: %s", name, w.Message)})
				case "CreateContainerConfigError", "CreateContainerError":
					failures = append(failures, Failure{ReasonConfigError, fmt.Sprintf("%s: %s", name, w.Message)})
				}
			}
			if t == nil || t.ExitCode == 0 {
				continue
			}
			switch {
			case t.Reason == "OOMKilled":
				failures = append(failures, Failure{ReasonOOMKilled, fmt.Sprintf("%s ran out of memory", name)})
			case notReady[p.Spec.NodeName] && (t.ExitCode == 137 || t.ExitCode == 143):
				failures = append(failures, Failure{ReasonNodeFailure,
					fmt.Sprintf("%s was killed on node %s, which is not ready", name, p.Spec.NodeName)})
			default:
				msg := fmt.Sprintf("%s exited with code %d", name, t.ExitCode)
				if t.Message != "" {
					msg += ": " + lastLine(t.Message)
				}
				failures = append(failures, Failure{ReasonExitCode, msg})
			}
		}
		if p.Status.Phase != corev1.PodSucceeded && notReady[p.Spec.NodeName] {
			msg := fmt.Sprintf("%s: node %s is not ready", name, p.Spec.NodeName)
			failures = append(failures, Failure{ReasonNodeFailure, msg})
		}
	}
	if len(failures) == 0 {
		return Failure{ReasonUnknown, ""}
	}
	return byPriority(failures)
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// stuckWaiting are container waiting reasons a job does not recover from by itself.
var stuckWaiting = map[string]string{
	"ErrImagePull": ReasonImagePullFailed, "ImagePullBackOff": ReasonImagePullFailed,
	"InvalidImageName": ReasonImagePullFailed, "CreateContainerConfigError": ReasonConfigError,
	"CreateContainerError": ReasonConfigError, "CrashLoopBackOff": ReasonExitCode,
}

// Stuck reports a running job whose pods have been unable to start for
// longer than timeout. It returns the failure and, when not stuck yet, how
// long until it would be.
func Stuck(pods []corev1.Pod, now time.Time, timeout time.Duration) (*Failure, time.Duration) {
	var next time.Duration
	for i := range pods {
		p := &pods[i]
		if !p.DeletionTimestamp.IsZero() || p.Status.Phase != corev1.PodPending && p.Status.Phase != corev1.PodRunning {
			continue
		}
		for _, cs := range p.Status.ContainerStatuses {
			w := cs.State.Waiting
			if w == nil {
				continue
			}
			reason, ok := stuckWaiting[w.Reason]
			if !ok {
				continue
			}
			since := scheduledTime(p)
			if left := timeout - now.Sub(since); left > 0 {
				if next == 0 || left < next {
					next = left
				}
				continue
			}
			f := Classify([]corev1.Pod{*p}, nil)
			if f.Reason == ReasonUnknown {
				f = Failure{reason, fmt.Sprintf("%s: %s", podName(p), w.Reason)}
			}
			f.Message = fmt.Sprintf("%s (stuck for %s)", f.Message, now.Sub(since).Round(time.Minute))
			return &f, 0
		}
	}
	return nil, next
}

func scheduledTime(p *corev1.Pod) time.Time {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionTrue {
			return c.LastTransitionTime.Time
		}
	}
	return p.CreationTimestamp.Time
}

// RetryName is the name of retry attempt n of a job group.
func RetryName(group string, n int) string { return fmt.Sprintf("%s-r%d", group, n) }

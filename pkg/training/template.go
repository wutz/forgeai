package training

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/scclabs/forgeai/pkg/kf"
)

func batchJobTemplate(image string, cmd []string, env []corev1.EnvVar) batchv1.JobTemplateSpec {
	return batchv1.JobTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{kf.LabelAncestorStep: kf.AncestorTrainer}},
		Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: kf.ContainerNode, Image: image, Command: cmd, Env: env}},
		}}},
	}
}

package resourcegenerator

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"packer-plugin-kubevirt/builder/common/k8s"
)

func GenerateInitJob(name, ns string) *batchv1.Job {
	ttlSecondsAfterFinish := int32(30)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "init",
			Namespace: ns,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttlSecondsAfterFinish,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{
						k8s.ImageBuilderTaintKey: k8s.ImageBuilderTaintValue,
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      k8s.ImageBuilderTaintKey,
							Operator: corev1.TolerationOpEqual,
							Value:    k8s.ImageBuilderTaintValue,
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "busybox",
							Image: "yauritux/busybox-curl",
							Command: []string{
								"sleep",
								"600",
							},
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
				},
			},
		},
	}
}

package generator

import (
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"packer-plugin-kubevirt/builder/common/k8s"
	"strconv"
)

// GenerateInitJob is helping provision a node when a VirtualMachine needs to be scheduled. A 'Job' is preferred over
// a 'Pod' to get the TTL controller cleaning up the resource as soon as it reaches a 'Finished' state.
func GenerateInitJob(ns, name string, ttlInSeconds int) *batchv1.Job {
	ttlAfterFinished := int32(0)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-init", name),
			Namespace: ns,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttlAfterFinished,
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
								strconv.Itoa(ttlInSeconds),
							},
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
				},
			},
		},
	}
}

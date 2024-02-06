package generator

import (
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"packer-plugin-kubevirt/builder/common/k8s"
	"strconv"
	"time"
)

// GenerateInitJob is helping provision a node when a KubeVirt object needs to be scheduled. A 'Job' is preferred over
// a 'Pod' to get the TTL controller cleaning up the resource as soon as it gets to a 'Finished' state.
func GenerateInitJob(ns, name string, ttl time.Duration, autoscaler k8s.NodeAutoscaler) *batchv1.Job {
	ttlAfterFinished := int32(0)

	var nodeSelector map[string]string
	var tolerations []corev1.Toleration
	switch autoscaler {
	case k8s.DefaultNodeAutoscaler:
		nodeSelector = nil
		tolerations = nil
	case k8s.KarpenterNodeAutoscaler:
		nodeSelector = map[string]string{
			k8s.ImageBuilderTaintKey: k8s.ImageBuilderTaintValue,
		}
		tolerations = []corev1.Toleration{
			{
				Key:      k8s.ImageBuilderTaintKey,
				Operator: corev1.TolerationOpEqual,
				Value:    k8s.ImageBuilderTaintValue,
			},
		}
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-init", name),
			Namespace: ns,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttlAfterFinished,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector:  nodeSelector,
					Tolerations:   tolerations,
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "busybox",
							Image: "yauritux/busybox-curl",
							Command: []string{
								"sleep",
								strconv.Itoa(int(ttl.Seconds())),
							},
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
				},
			},
		},
	}
}

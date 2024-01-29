package generator

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"packer-plugin-kubevirt/builder/common/k8s"
	"strconv"
)

func GenerateInitPod(ns, name string, ttlInSeconds int) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-init", name),
			Namespace: ns,
		},
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
	}
}

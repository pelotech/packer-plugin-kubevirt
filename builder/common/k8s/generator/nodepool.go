package generator

import (
	awsv1beta1 "github.com/aws/karpenter/pkg/apis/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"packer-plugin-kubevirt/builder/common/k8s"
	"sigs.k8s.io/karpenter/pkg/apis/v1beta1"
	"time"
)

func GenerateNodePool() *v1beta1.NodePool {
	disruptionAfter := 720 * time.Hour

	return &v1beta1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vm-image-builder",
		},
		Spec: v1beta1.NodePoolSpec{
			Disruption: v1beta1.Disruption{
				ConsolidationPolicy: v1beta1.ConsolidationPolicyWhenUnderutilized,
				ExpireAfter: v1beta1.NillableDuration{
					Duration: &disruptionAfter,
				},
			},
			Template: v1beta1.NodeClaimTemplate{
				ObjectMeta: v1beta1.ObjectMeta{
					Labels: map[string]string{
						k8s.ImageBuilderTaintKey:   k8s.ImageBuilderTaintValue,
						kubevirtv1.NodeSchedulable: "true",
					},
				},
				Spec: v1beta1.NodeClaimSpec{
					NodeClassRef: &v1beta1.NodeClassReference{
						APIVersion: v1beta1.SchemeGroupVersion.String(),
						Kind:       "EC2NodeClass",
						Name:       "default",
					},
					Taints: []corev1.Taint{
						{
							Key:    k8s.ImageBuilderTaintKey,
							Value:  k8s.ImageBuilderTaintValue,
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
					Requirements: []corev1.NodeSelectorRequirement{
						{
							Key:      corev1.LabelArchStable,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{v1beta1.ArchitectureAmd64},
						},
						{
							Key:      corev1.LabelOSStable,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{string(corev1.Linux)},
						},
						{
							Key:      v1beta1.CapacityTypeLabelKey,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{v1beta1.CapacityTypeSpot},
						},
						{
							Key:      awsv1beta1.LabelInstanceCategory,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"c", "m", "r"},
						},
						{
							Key:      awsv1beta1.LabelInstanceGeneration,
							Operator: corev1.NodeSelectorOpGt,
							Values:   []string{"4"},
						},
						{
							Key:      awsv1beta1.LabelInstanceSize,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"metal"},
						},
					},
				},
			},
		},
	}
}

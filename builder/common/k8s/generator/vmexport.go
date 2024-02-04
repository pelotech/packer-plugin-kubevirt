package generator

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1alpha1"
	"packer-plugin-kubevirt/builder/common/k8s"
)

const (
	tokenSecretSuffix = "export-token"
	tokenSecretKey    = "token"
)

func GenerateTokenSecret(export *exportv1.VirtualMachineExport, token string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildTokenSecretName(export.Spec.Source.Name),
			Namespace: export.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(export, exportv1.SchemeGroupVersion.WithKind(k8s.VirtualMachineExportKind)),
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			tokenSecretKey: token,
		},
	}
}

func GenerateVirtualMachineExport(vm *kubevirtv1.VirtualMachine) *exportv1.VirtualMachineExport {
	exportSource := corev1.TypedLocalObjectReference{
		APIGroup: &kubevirtv1.VirtualMachineGroupVersionKind.Group,
		Kind:     kubevirtv1.VirtualMachineGroupVersionKind.Kind,
		Name:     vm.Name,
	}
	secretName := buildTokenSecretName(vm.Name)

	return &exportv1.VirtualMachineExport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vm.Name,
			Namespace: vm.Namespace,
		},
		Spec: exportv1.VirtualMachineExportSpec{
			TokenSecretRef: &secretName,
			Source:         exportSource,
		},
	}
}

func buildTokenSecretName(vmName string) string {
	return fmt.Sprintf("%s-%s", vmName, tokenSecretSuffix)
}

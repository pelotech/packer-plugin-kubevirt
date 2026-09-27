package generator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"testing"
)

func TestGenerateTokenSecretIsOwnedByExport(t *testing.T) {
	export := GenerateVirtualMachineExport(&kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
	})
	export.UID = "export-uid"

	secret := GenerateTokenSecret(export, "token")

	owner := metav1.GetControllerOf(secret)
	if owner == nil || owner.APIVersion != "export.kubevirt.io/v1" || owner.Kind != "VirtualMachineExport" || owner.UID != export.UID {
		t.Errorf("expected the secret to be owned by the 'v1' export, got: %v", secret.OwnerReferences)
	}
}

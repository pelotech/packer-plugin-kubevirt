package generator

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"reflect"
	"testing"
	"time"
)

func TestGenerateTokenSecretIsOwnedByExport(t *testing.T) {
	export := GenerateVirtualMachineExport(&kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
	}, 0)
	export.UID = "export-uid"

	secret := GenerateTokenSecret(export, "token")

	owner := metav1.GetControllerOf(secret)
	if owner == nil || owner.APIVersion != "export.kubevirt.io/v1" || owner.Kind != "VirtualMachineExport" || owner.UID != export.UID {
		t.Errorf("expected the secret to be owned by the 'v1' export, got: %v", secret.OwnerReferences)
	}
}

func TestGenerateVirtualMachineExportTTL(t *testing.T) {
	for name, test := range map[string]struct {
		ttl      time.Duration
		expected *metav1.Duration
	}{
		"unset, KubeVirt applies its default": {ttl: 0, expected: nil},
		"set":                                 {ttl: 6 * time.Hour, expected: &metav1.Duration{Duration: 6 * time.Hour}},
	} {
		t.Run(name, func(t *testing.T) {
			export := GenerateVirtualMachineExport(&kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
			}, test.ttl)

			if !reflect.DeepEqual(export.Spec.TTLDuration, test.expected) {
				t.Errorf("expected the TTL %v, got: %v", test.expected, export.Spec.TTLDuration)
			}
		})
	}
}

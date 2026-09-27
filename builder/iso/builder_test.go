package iso

import (
	"strings"
	"testing"
)

func TestPrepareRejectsInvalidResources(t *testing.T) {
	for _, field := range []string{"vm_cpu", "vm_memory"} {
		builder := new(Builder)
		_, _, err := builder.Prepare(map[string]interface{}{
			"kubernetes_name": "base-ubuntu",
			"vm_disk_space":   "10Gi",
			field:             "plenty",
		})
		if err == nil || !strings.Contains(err.Error(), "invalid '"+field+"' value 'plenty'") {
			t.Errorf("expected an invalid '%s' error, got: %v", field, err)
		}
	}
}

func TestDecodeTolerations(t *testing.T) {
	tolerations := decodeTolerations([]map[string]string{
		{"key": "pelo.tech/kvm", "operator": "Equal", "value": "true", "effect": "NoSchedule"},
	})

	if len(tolerations) != 1 {
		t.Fatalf("expected one toleration, got: %v", tolerations)
	}
	toleration := tolerations[0]
	if toleration.Key != "pelo.tech/kvm" || toleration.Operator != "Equal" || toleration.Value != "true" || toleration.Effect != "NoSchedule" {
		t.Errorf("unexpected toleration: %+v", toleration)
	}
}

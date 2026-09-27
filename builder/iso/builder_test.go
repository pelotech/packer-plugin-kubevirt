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

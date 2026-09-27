package common

import (
	"context"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	"packer-plugin-kubevirt/builder/common/k8s"
	"testing"
)

func TestDeleteOrKeepExport(t *testing.T) {
	for name, test := range map[string]struct {
		keep          bool
		expectedCount int
	}{
		"deleted by default": {keep: false, expectedCount: 0},
		"kept when asked":    {keep: true, expectedCount: 1},
	} {
		t.Run(name, func(t *testing.T) {
			export := newExport()
			clients := &k8s.Clients{Kubevirt: kubevirtfake.NewSimpleClientset(export)}

			DeleteOrKeepExport(clients, packersdk.TestUi(t), export.Namespace, export.Name, test.keep)

			exports, err := clients.Kubevirt.ExportV1().VirtualMachineExports(export.Namespace).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("failed to list Virtual Machine Exports: %v", err)
			}
			if len(exports.Items) != test.expectedCount {
				t.Errorf("expected %d Virtual Machine Export, got: %d", test.expectedCount, len(exports.Items))
			}
		})
	}
}

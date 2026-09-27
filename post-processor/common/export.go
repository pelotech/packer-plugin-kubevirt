package common

import (
	"context"
	"fmt"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"packer-plugin-kubevirt/builder/common/k8s"
)

func DeleteOrKeepExport(clients *k8s.Clients, ui packersdk.Ui, namespace, name string, keep bool) {
	if keep {
		ui.Message(fmt.Sprintf("Virtual Machine Export %s/%s has been kept", namespace, name))
		return
	}

	err := clients.Kubevirt.ExportV1().VirtualMachineExports(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err == nil {
		ui.Message(fmt.Sprintf("Virtual Machine Export %s/%s has been deleted", namespace, name))
	} else {
		ui.Error(fmt.Sprintf("failed to delete Virtual Machine Export %s/%s: %v", namespace, name, err))
	}
}

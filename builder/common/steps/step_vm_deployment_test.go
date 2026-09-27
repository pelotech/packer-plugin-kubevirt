package steps

import (
	"context"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"testing"
)

func TestStepDeployVMCleanup(t *testing.T) {
	for name, test := range map[string]struct {
		ownedByExport bool
		failure       string
		keepVm        bool
	}{
		"owned by its export, successful build": {ownedByExport: true, keepVm: true},
		"owned by its export, halted build":     {ownedByExport: true, failure: multistep.StateHalted},
		"owned by its export, cancelled build":  {ownedByExport: true, failure: multistep.StateCancelled},
		"not handed over":                       {ownedByExport: false},
	} {
		t.Run(name, func(t *testing.T) {
			vm := &kubevirtv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"}}
			kubevirtClient := kubevirtfake.NewSimpleClientset(vm)
			appContext := &common.AppContext{State: new(multistep.BasicStateBag)}
			appContext.Put(common.PackerUi, packersdk.TestUi(t))
			appContext.Put(common.VirtualMachine, vm)
			if test.ownedByExport {
				appContext.Put(common.VirtualMachineOwnedByExport, true)
			}
			if test.failure != "" {
				appContext.State.Put(test.failure, true)
			}

			step := &StepDeployVM{Clients: &k8s.Clients{Kubevirt: kubevirtClient}}
			step.Cleanup(appContext.State)

			_, err := kubevirtClient.KubevirtV1().VirtualMachines(vm.Namespace).Get(context.Background(), vm.Name, metav1.GetOptions{})
			if kept := err == nil; kept != test.keepVm {
				t.Errorf("expected the Virtual Machine to be kept: %t, got: %t (%v)", test.keepVm, kept, err)
			}
		})
	}
}

package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	corev1 "k8s.io/api/core/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"time"
)

type StepWaitForVM struct {
	Clients          *k8s.Clients
	VmInstallTimeOut time.Duration
}

func (s *StepWaitForVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	ui := appContext.GetPackerUi()
	ns := appContext.GetVirtualMachine().Namespace
	name := appContext.GetVirtualMachine().Name

	err := s.waitForVirtualMachine(ui, ns, name)
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to wait to be in a 'Ready' state for Virtual Machine %s/%s: %s", ns, name, err))
	}

	ui.Say(fmt.Sprintf("install step has completed for Virtual Machine %s/%s", ns, name))

	return multistep.ActionContinue
}

func isReady(vm *kubevirtv1.VirtualMachine) bool {
	for _, condition := range vm.Status.Conditions {
		if condition.Type == kubevirtv1.VirtualMachineReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// waitForVirtualMachine starts from the current state, the Virtual Machine may have got ready while the boot command was typed
func (s *StepWaitForVM) waitForVirtualMachine(ui packer.Ui, ns, name string) error {
	vms := s.Clients.Kubevirt.KubevirtV1().VirtualMachines(ns)
	return k8s.WaitForResource(context.TODO(), s.Clients.Kubevirt, vms, name, s.VmInstallTimeOut, func(vm *kubevirtv1.VirtualMachine) (bool, error) {
		if isReady(vm) {
			return true, nil
		}
		if conditions := vm.Status.Conditions; len(conditions) > 0 {
			last := conditions[len(conditions)-1]
			ui.Message(fmt.Sprintf("condition '%s' is '%s'", last.Type, last.Status))
			ui.Message(fmt.Sprintf("message: %s", last.Message))
		}
		return false, nil
	})
}

func (s *StepWaitForVM) Cleanup(_ multistep.StateBag) {}

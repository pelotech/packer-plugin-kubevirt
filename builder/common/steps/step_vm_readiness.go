package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
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

	// the watch starts from the current version, the one of the creation may be too old by now
	vm, err := s.Clients.Kubevirt.KubevirtV1().VirtualMachines(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to get Virtual Machine %s/%s: %s", ns, name, err))
	}

	if !isReady(vm) {
		err = s.waitForVirtualMachine(ui, vm)
		if err != nil {
			return appContext.Halt(fmt.Errorf("failed to wait to be in a 'Ready' state for Virtual Machine %s/%s: %s", ns, name, err))
		}
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

func (s *StepWaitForVM) waitForVirtualMachine(ui packer.Ui, vm *kubevirtv1.VirtualMachine) error {
	watchFunc := func(event watch.Event) (bool, error) {
		vm, ok := event.Object.(*kubevirtv1.VirtualMachine)
		if !ok {
			return false, fmt.Errorf("unexpected type for %v", event.Object)
		}
		if isReady(vm) {
			return true, nil
		}
		if conditions := vm.Status.Conditions; len(conditions) > 0 {
			last := conditions[len(conditions)-1]
			ui.Message(fmt.Sprintf("condition '%s' is '%s'", last.Type, last.Status))
			ui.Message(fmt.Sprintf("message: %s", last.Message))
		}
		return false, nil
	}
	return k8s.WaitForResource(s.Clients.Kubevirt.KubevirtV1().RESTClient(), vm.Namespace, k8s.VirtualMachineResourceName, vm.Name, vm.ResourceVersion, s.VmInstallTimeOut, watchFunc)
}

func (s *StepWaitForVM) Cleanup(_ multistep.StateBag) {}

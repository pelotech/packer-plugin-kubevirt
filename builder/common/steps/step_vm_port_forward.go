package steps

import (
	"context"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"kubevirt.io/client-go/kubecli"
	"packer-plugin-kubevirt/builder/common"
)

type StepPortForwardVM struct {
	VirtClient kubecli.KubevirtClient
}

func (s *StepPortForwardVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	vm := appContext.GetVirtualMachine()

	// TODO: to revamp
	_, err := s.VirtClient.VirtualMachineInstance(vm.Namespace).PortForward(vm.Name, 22, "TCP")
	if err != nil {
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *StepPortForwardVM) Cleanup(_ multistep.StateBag) {
}

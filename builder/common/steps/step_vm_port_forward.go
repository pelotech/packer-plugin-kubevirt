package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"kubevirt.io/client-go/kubecli"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
)

type StepPortForwardVM struct {
	VirtClient   kubecli.KubevirtClient
	PortMappings []string
	stopChan     chan struct{}
}

func (s *StepPortForwardVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	vm := appContext.GetVirtualMachine()

	stopChan, err := k8s.RunAsyncPortForward(s.VirtClient, vm.Name, vm.Namespace, s.PortMappings)
	if err != nil {
		err := fmt.Errorf("failed to port-forward Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err)
		appContext.Put(common.PackerError, err)
		appContext.GetPackerUi().Error(err.Error())

		return multistep.ActionHalt
	}
	s.stopChan = stopChan

	return multistep.ActionContinue
}

func (s *StepPortForwardVM) Cleanup(_ multistep.StateBag) {
	if s.stopChan != nil {
		close(s.stopChan)
	}
}

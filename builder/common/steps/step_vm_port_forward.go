package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
)

type StepPortForwardVM struct {
	VirtClient   kubecli.KubevirtClient
	PortMappings []string
	stopChan     chan struct{}
}

func (s *StepPortForwardVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	ui := appContext.GetPackerUi()
	vm := appContext.GetVirtualMachine()
	pods, err := s.VirtClient.CoreV1().Pods(vm.Namespace).List(ctx, v1.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			kubevirtv1.VirtualMachineNameLabel: vm.Name,
		}).String(),
	})
	if err != nil || len(pods.Items) < 1 {
		err := fmt.Errorf("failed to get pod name for port-forwarding Virtual Machine %s/%s: %w", vm.Namespace, vm.Name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	stopChan, err := k8s.RunAsyncPortForward(s.VirtClient, pods.Items[0].Name, vm.Namespace, s.PortMappings)
	if err != nil {
		err := fmt.Errorf("failed to port-forward Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}
	s.stopChan = stopChan

	ui.Say(fmt.Sprintf("port-forwarding step has completed for Virtual Machine %s/%s", vm.Namespace, vm.Name))

	return multistep.ActionContinue
}

func (s *StepPortForwardVM) Cleanup(_ multistep.StateBag) {
	if s.stopChan != nil {
		close(s.stopChan)
	}
}

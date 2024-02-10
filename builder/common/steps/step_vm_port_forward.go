package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
)

type StepPortForwardVM struct {
	VirtClient kubecli.KubevirtClient
	Comm       communicator.Config
	stopChan chan struct{}
}

func (s *StepPortForwardVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	ui := appContext.GetPackerUi()

	portMappings, err := s.computePortMappings()
	if err != nil {
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

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

	stopChan, err := k8s.RunAsyncPortForward(s.VirtClient, pods.Items[0].Name, vm.Namespace, portMappings)
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

func (s *StepPortForwardVM) computePortMappings() ([]string, error) {
	var portMapping string
	switch s.Comm.Type {
	case "ssh":
		portMapping = fmt.Sprintf("%d:%d", common.GetOrDefault(s.Comm.SSHPort, common.DefaultSSHPort), common.DefaultSSHPort)
	case "winrm":
		// NOTE: sysprep has the current DefaultWinRMPort value hardcoded, please change that value carefully while the sysprep conf. is not templated.
		portMapping = fmt.Sprintf("%d:%d", common.GetOrDefault(s.Comm.WinRMPort, common.DefaultWinRMPort), common.DefaultWinRMPort)
	default:
		return nil, fmt.Errorf("unsupported communicator type, allowed values: 'ssh', 'winrm'")
	}

	return []string{portMapping}, nil
}

func (s *StepPortForwardVM) Cleanup(_ multistep.StateBag) {
	if s.stopChan != nil {
		close(s.stopChan)
	}
}

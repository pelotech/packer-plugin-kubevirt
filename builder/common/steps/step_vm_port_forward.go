package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
)

type StepPortForwardVM struct {
	Clients  *k8s.Clients
	Comm     *communicator.Config
	stopChan chan struct{}
}

func (s *StepPortForwardVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	ui := appContext.GetPackerUi()

	portMappings, err := s.computePortMappings()
	if err != nil {
		return appContext.Halt(err)
	}

	vm := appContext.GetVirtualMachine()
	pods, err := s.Clients.Kubernetes.CoreV1().Pods(vm.Namespace).List(ctx, v1.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			kubevirtv1.DeprecatedVirtualMachineNameLabel: vm.Name,
		}).String(),
	})
	if err != nil || len(pods.Items) < 1 {
		return appContext.Halt(fmt.Errorf("failed to get pod name for port-forwarding Virtual Machine %s/%s: %w", vm.Namespace, vm.Name, err))
	}

	stopChan, err := k8s.RunAsyncPortForward(s.Clients, pods.Items[0].Name, vm.Namespace, portMappings)
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to port-forward Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err))
	}
	s.stopChan = stopChan

	ui.Say(fmt.Sprintf("port-forwarding step has completed for Virtual Machine %s/%s on local port %d", vm.Namespace, vm.Name, s.Comm.Port()))

	return multistep.ActionContinue
}

func (s *StepPortForwardVM) computePortMappings() ([]string, error) {
	localPort, remotePort := &s.Comm.SSHPort, common.DefaultSSHPort
	if s.Comm.Type == "winrm" {
		// NOTE: the default autounattend.xml has the current DefaultWinRMPort value hardcoded, please change that value carefully while the answer file is not templated.
		localPort, remotePort = &s.Comm.WinRMPort, common.DefaultWinRMPort
	}
	if *localPort == 0 {
		freePort, err := common.FindFreePort()
		if err != nil {
			return nil, err
		}
		*localPort = freePort
	}

	return []string{fmt.Sprintf("%d:%d", *localPort, remotePort)}, nil
}

func (s *StepPortForwardVM) Cleanup(_ multistep.StateBag) {
	if s.stopChan != nil {
		close(s.stopChan)
	}
}

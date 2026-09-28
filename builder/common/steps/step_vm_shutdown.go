package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"time"
)

type StepShutdownVM struct {
	Clients         *k8s.Clients
	ShutdownCommand string
	ShutdownTimeout time.Duration
}

func (s *StepShutdownVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	if s.ShutdownCommand == "" {
		return multistep.ActionContinue
	}
	ui := appContext.GetPackerUi()
	vm := appContext.GetVirtualMachine()
	communicator := state.Get("communicator").(packersdk.Communicator)

	ui.Say(fmt.Sprintf("running the shutdown command in Virtual Machine %s/%s...", vm.Namespace, vm.Name))
	commandEnd := make(chan error, 1)
	go func() {
		command := &packersdk.RemoteCmd{Command: s.ShutdownCommand}
		commandEnd <- command.RunWithUi(ctx, communicator, ui)
	}()
	stopped := make(chan error, 1)
	go func() {
		stopped <- k8s.WaitForVirtualMachineStopped(ctx, s.Clients.Kubevirt.KubevirtV1().VirtualMachines(vm.Namespace), vm.Name, s.ShutdownTimeout)
	}()

	// the connection is lost when the guest shuts down: only a stopped Virtual Machine tells that the command worked
	var commandErr error
	for {
		select {
		case commandErr = <-commandEnd:
			if commandErr != nil {
				ui.Message(fmt.Sprintf("the shutdown command ended with: %s", commandErr))
			}
			commandEnd = nil
		case err := <-stopped:
			if err != nil && commandErr != nil {
				return appContext.Halt(fmt.Errorf("Virtual Machine %s/%s is still running after the shutdown command, which ended with: %s", vm.Namespace, vm.Name, commandErr))
			}
			if err != nil {
				return appContext.Halt(fmt.Errorf("Virtual Machine %s/%s is still running after the shutdown command: %s", vm.Namespace, vm.Name, err))
			}
			ui.Say(fmt.Sprintf("shutdown step has completed for Virtual Machine %s/%s", vm.Namespace, vm.Name))
			return multistep.ActionContinue
		}
	}
}

func (s *StepShutdownVM) Cleanup(_ multistep.StateBag) {}

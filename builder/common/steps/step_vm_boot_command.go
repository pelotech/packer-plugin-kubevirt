package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/bootcommand"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/mitchellh/go-vnc"
	"net"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"time"
)

type StepBootCommand struct {
	Clients          *k8s.Clients
	BootCommand      []string
	BootWait         time.Duration
	KeyInterval      time.Duration
	KeyGroupInterval time.Duration
	Timeout          time.Duration

	openConsole func(clients *k8s.Clients, namespace, name string) (net.Conn, error)
}

func (s *StepBootCommand) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	if len(s.BootCommand) == 0 {
		return multistep.ActionContinue
	}
	ui := appContext.GetPackerUi()
	vm := appContext.GetVirtualMachine()

	ui.Say(fmt.Sprintf("waiting for Virtual Machine %s/%s to run...", vm.Namespace, vm.Name))
	err := k8s.WaitForVirtualMachineInstanceRunning(ctx, s.Clients.Kubevirt.KubevirtV1().VirtualMachineInstances(vm.Namespace), vm.Name, s.Timeout)
	if err != nil {
		return appContext.Halt(fmt.Errorf("Virtual Machine %s/%s is not running: %s", vm.Namespace, vm.Name, err))
	}

	select {
	case <-time.After(s.BootWait):
	case <-ctx.Done():
		return appContext.Halt(ctx.Err())
	}

	openConsole := s.openConsole
	if openConsole == nil {
		openConsole = k8s.OpenConsole
	}
	connection, err := openConsole(s.Clients, vm.Namespace, vm.Name)
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to open the console of Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err))
	}
	// the console has no timeout of its own
	err = connection.SetDeadline(time.Now().Add(s.Timeout))
	if err != nil {
		_ = connection.Close()
		return appContext.Halt(fmt.Errorf("failed to open the console of Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err))
	}
	console, err := vnc.Client(connection, &vnc.ClientConfig{})
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to open the console of Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err))
	}
	defer console.Close()

	ui.Say(fmt.Sprintf("typing the boot command in Virtual Machine %s/%s...", vm.Namespace, vm.Name))
	driver := bootcommand.NewVNCDriver(console, s.KeyInterval)
	for _, group := range s.BootCommand {
		err = typeKeys(ctx, driver, group, s.KeyGroupInterval)
		if err != nil {
			return appContext.Halt(fmt.Errorf("failed to type the boot command in Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err))
		}
	}

	return multistep.ActionContinue
}

func typeKeys(ctx context.Context, driver bootcommand.BCDriver, group string, interval time.Duration) error {
	keys, err := bootcommand.GenerateExpressionSequence(group)
	if err != nil {
		return err
	}
	if err = keys.Do(ctx, driver); err != nil {
		return err
	}

	select {
	case <-time.After(interval):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *StepBootCommand) Cleanup(_ multistep.StateBag) {}

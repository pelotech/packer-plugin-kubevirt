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
	"strings"
	"testing"
	"time"
)

// blockingCommunicator never reports the end of a command, as a connection lost with the guest
type blockingCommunicator struct {
	packersdk.MockCommunicator
	commands chan string
}

func (c *blockingCommunicator) Start(_ context.Context, command *packersdk.RemoteCmd) error {
	c.commands <- command.Command
	return nil
}

func newShutdownStep(t *testing.T, status kubevirtv1.VirtualMachinePrintableStatus, command string) (*StepShutdownVM, multistep.StateBag, *blockingCommunicator, *kubevirtfake.Clientset) {
	t.Helper()
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-windows", Namespace: "packer"},
		Status:     kubevirtv1.VirtualMachineStatus{PrintableStatus: status},
	}
	communicator := &blockingCommunicator{commands: make(chan string, 1)}
	appContext := &common.AppContext{State: new(multistep.BasicStateBag)}
	appContext.Put(common.PackerUi, packersdk.TestUi(t))
	appContext.Put(common.VirtualMachine, vm)
	appContext.State.Put("communicator", communicator)

	kubevirtClient := kubevirtfake.NewSimpleClientset(vm)
	step := &StepShutdownVM{
		Clients:         &k8s.Clients{Kubevirt: kubevirtClient},
		ShutdownCommand: command,
		ShutdownTimeout: 3 * time.Second,
	}
	return step, appContext.State, communicator, kubevirtClient
}

func TestStepShutdownVMWaitsForTheGuestToStop(t *testing.T) {
	step, state, communicator, kubevirtClient := newShutdownStep(t, kubevirtv1.VirtualMachineStatusRunning, "sysprep.exe /generalize /oobe /shutdown")

	go func() {
		time.Sleep(200 * time.Millisecond)
		vmClient := kubevirtClient.KubevirtV1().VirtualMachines("packer")
		vm, _ := vmClient.Get(context.Background(), "base-windows", metav1.GetOptions{})
		vm.Status.PrintableStatus = kubevirtv1.VirtualMachineStatusStopped
		_, _ = vmClient.UpdateStatus(context.Background(), vm, metav1.UpdateOptions{})
	}()

	started := time.Now()
	if action := step.Run(context.Background(), state); action != multistep.ActionContinue {
		t.Fatalf("expected the step to continue, got action: %v, error: %v", action, state.Get(string(common.PackerError)))
	}
	select {
	case command := <-communicator.commands:
		if command != "sysprep.exe /generalize /oobe /shutdown" {
			t.Errorf("expected the shutdown command to run in the guest, got: '%s'", command)
		}
	default:
		t.Errorf("expected the shutdown command to run in the guest")
	}
	if time.Since(started) < 200*time.Millisecond {
		t.Errorf("expected the step to wait for the Virtual Machine to be stopped")
	}
}

func TestStepShutdownVMWithoutCommandLeavesTheGuestAlone(t *testing.T) {
	step, state, communicator, _ := newShutdownStep(t, kubevirtv1.VirtualMachineStatusRunning, "")

	if action := step.Run(context.Background(), state); action != multistep.ActionContinue {
		t.Fatalf("expected the step to continue, got action: %v", action)
	}
	select {
	case command := <-communicator.commands:
		t.Errorf("expected no command to run in the guest, got: '%s'", command)
	default:
	}
}

func TestStepShutdownVMGivesUpOnAGuestThatKeepsRunning(t *testing.T) {
	step, state, _, _ := newShutdownStep(t, kubevirtv1.VirtualMachineStatusRunning, "echo")
	step.ShutdownTimeout = 200 * time.Millisecond

	if action := step.Run(context.Background(), state); action != multistep.ActionHalt {
		t.Fatalf("expected the step to halt, got action: %v", action)
	}
	err, _ := state.Get(string(common.PackerError)).(error)
	if err == nil || !strings.Contains(err.Error(), "shutdown command") {
		t.Errorf("expected an error about the shutdown command, got: %v", err)
	}
}

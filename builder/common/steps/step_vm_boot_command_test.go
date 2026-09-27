package steps

import (
	"context"
	"encoding/binary"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	"net"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	keyUp    uint32 = 0xFF52
	keyEnter uint32 = 0xFF0D
)

type keyEvent struct {
	Key  uint32
	Down bool
}

// serveConsole answers the VNC handshake, then records the keys until the connection is closed
func serveConsole(connection net.Conn, keys chan<- []keyEvent) {
	defer connection.Close()
	var received []keyEvent
	defer func() { keys <- received }()

	version := make([]byte, 12)
	_, _ = connection.Write([]byte("RFB 003.008\n"))
	if _, err := io.ReadFull(connection, version); err != nil {
		return
	}
	// one security type is offered: none
	_, _ = connection.Write([]byte{1, 1})
	choice := make([]byte, 1)
	if _, err := io.ReadFull(connection, choice); err != nil {
		return
	}
	_ = binary.Write(connection, binary.BigEndian, uint32(0))
	if _, err := io.ReadFull(connection, choice); err != nil {
		return
	}
	// screen size, pixel format and an empty name
	_, _ = connection.Write(make([]byte, 2+2+16+4))

	for {
		message := make([]byte, 8)
		if _, err := io.ReadFull(connection, message); err != nil {
			return
		}
		received = append(received, keyEvent{Key: binary.BigEndian.Uint32(message[4:]), Down: message[1] == 1})
	}
}

func newBootCommandStep(t *testing.T, instance *kubevirtv1.VirtualMachineInstance, bootCommand []string) (*StepBootCommand, multistep.StateBag, chan []keyEvent) {
	t.Helper()
	vm := &kubevirtv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Name: "base-windows", Namespace: "packer"}}
	appContext := &common.AppContext{State: new(multistep.BasicStateBag)}
	appContext.Put(common.PackerUi, packersdk.TestUi(t))
	appContext.Put(common.VirtualMachine, vm)

	keys := make(chan []keyEvent, 1)
	step := &StepBootCommand{
		Clients:     &k8s.Clients{Kubevirt: kubevirtfake.NewSimpleClientset(instance)},
		BootCommand: bootCommand,
		BootWait:    10 * time.Millisecond,
		KeyInterval: time.Millisecond,
		Timeout:     2 * time.Second,
		openConsole: func(_ *k8s.Clients, namespace, name string) (net.Conn, error) {
			if namespace != "packer" || name != "base-windows" {
				t.Errorf("expected the console of 'packer/base-windows', got: '%s/%s'", namespace, name)
			}
			client, server := net.Pipe()
			go serveConsole(server, keys)
			return client, nil
		},
	}
	return step, appContext.State, keys
}

func newInstance(phase kubevirtv1.VirtualMachineInstancePhase) *kubevirtv1.VirtualMachineInstance {
	return &kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "base-windows", Namespace: "packer"},
		Status:     kubevirtv1.VirtualMachineInstanceStatus{Phase: phase},
	}
}

func TestStepBootCommandTypesTheKeys(t *testing.T) {
	step, state, keys := newBootCommandStep(t, newInstance(kubevirtv1.Running), []string{"<up>", "<enter>"})

	if action := step.Run(context.Background(), state); action != multistep.ActionContinue {
		t.Fatalf("expected the step to continue, got action: %v, error: %v", action, state.Get(string(common.PackerError)))
	}

	expected := []keyEvent{{keyUp, true}, {keyUp, false}, {keyEnter, true}, {keyEnter, false}}
	select {
	case received := <-keys:
		if !reflect.DeepEqual(received, expected) {
			t.Errorf("expected keys %v, got: %v", expected, received)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected the console to be closed once the keys are typed")
	}
}

func TestStepBootCommandWithoutCommandLeavesTheConsoleAlone(t *testing.T) {
	step, state, _ := newBootCommandStep(t, newInstance(kubevirtv1.Running), nil)
	step.openConsole = func(*k8s.Clients, string, string) (net.Conn, error) {
		t.Errorf("expected the console to be left alone without a boot command")
		return nil, io.EOF
	}

	if action := step.Run(context.Background(), state); action != multistep.ActionContinue {
		t.Fatalf("expected the step to continue, got action: %v", action)
	}
}

func TestStepBootCommandWaitsForTheVirtualMachineToRun(t *testing.T) {
	step, state, _ := newBootCommandStep(t, newInstance(kubevirtv1.Scheduling), []string{"<enter>"})
	step.Timeout = 200 * time.Millisecond
	step.openConsole = func(*k8s.Clients, string, string) (net.Conn, error) {
		t.Errorf("expected no key to be typed before the Virtual Machine runs")
		return nil, io.EOF
	}

	if action := step.Run(context.Background(), state); action != multistep.ActionHalt {
		t.Fatalf("expected the step to halt, got action: %v", action)
	}
	err, _ := state.Get(string(common.PackerError)).(error)
	if err == nil || !strings.Contains(err.Error(), "is not running") {
		t.Errorf("expected an error about the Virtual Machine not running, got: %v", err)
	}
}

func TestStepBootCommandGivesUpOnASilentConsole(t *testing.T) {
	step, state, _ := newBootCommandStep(t, newInstance(kubevirtv1.Running), []string{"<enter>"})
	step.Timeout = 200 * time.Millisecond
	step.openConsole = func(*k8s.Clients, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		t.Cleanup(func() { _ = server.Close() })
		return client, nil
	}

	done := make(chan multistep.StepAction, 1)
	go func() { done <- step.Run(context.Background(), state) }()

	select {
	case action := <-done:
		if action != multistep.ActionHalt {
			t.Fatalf("expected the step to halt, got action: %v", action)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("expected the step to give up on a console that does not answer")
	}
}

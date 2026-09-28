package steps

import (
	"context"
	"errors"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	kubevirtv1 "kubevirt.io/api/core/v1"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	vmctx "packer-plugin-kubevirt/builder/common/vm"
	"slices"
	"testing"
	"time"
)

func TestStepGeneralizeRunsSysprepOnceVirtualMachineIsStopped(t *testing.T) {
	step, state, kubevirtClient, kubeClient := newGeneralizeStep(t, vmctx.Linux)

	var statusAtJobCreation kubevirtv1.VirtualMachinePrintableStatus
	kubeClient.PrependReactor("create", "jobs", func(k8stesting.Action) (bool, runtime.Object, error) {
		currentVm, err := kubevirtClient.KubevirtV1().VirtualMachines("packer").Get(context.Background(), "base-vm", metav1.GetOptions{})
		if err != nil {
			return true, nil, err
		}
		statusAtJobCreation = currentVm.Status.PrintableStatus
		return true, nil, errors.New("halt the step once the job creation is reached")
	})

	action := step.Run(context.Background(), state)

	if action != multistep.ActionHalt {
		t.Fatalf("expected the step to halt on job creation, got action: %v", action)
	}
	if statusAtJobCreation != kubevirtv1.VirtualMachineStatusStopped {
		t.Fatalf("expected the 'libguestfs' job to be created once the Virtual Machine is stopped, status was: '%s'", statusAtJobCreation)
	}
}

// newGeneralizeStep gives a running Virtual Machine that stops once asked to, and fails any job creation
func newGeneralizeStep(t *testing.T, osFamily vmctx.OsFamily) (*StepGeneralize, multistep.StateBag, *kubevirtfake.Clientset, *k8sfake.Clientset) {
	t.Helper()
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-vm", Namespace: "packer"},
		Spec:       kubevirtv1.VirtualMachineSpec{Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{}},
		Status:     kubevirtv1.VirtualMachineStatus{PrintableStatus: kubevirtv1.VirtualMachineStatusRunning},
	}

	kubevirtClient := kubevirtfake.NewSimpleClientset(vm)
	vmClient := kubevirtClient.KubevirtV1().VirtualMachines(vm.Namespace)
	kubevirtClient.PrependReactor("put", "virtualmachines/stop", func(k8stesting.Action) (bool, runtime.Object, error) {
		go func() {
			time.Sleep(100 * time.Millisecond)
			stoppedVm := vm.DeepCopy()
			stoppedVm.Status.PrintableStatus = kubevirtv1.VirtualMachineStatusStopped
			_, _ = vmClient.UpdateStatus(context.Background(), stoppedVm, metav1.UpdateOptions{})
		}()
		return true, nil, nil
	})
	kubeClient := k8sfake.NewSimpleClientset()
	kubeClient.PrependReactor("create", "jobs", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("no job expected")
	})

	appContext := &common.AppContext{State: new(multistep.BasicStateBag)}
	appContext.Put(common.PackerUi, packersdk.TestUi(t))
	appContext.Put(common.VirtualMachine, vm)

	step := &StepGeneralize{Clients: &k8s.Clients{Kubernetes: kubeClient, Kubevirt: kubevirtClient}, OsFamily: osFamily, VmExportTimeOut: 5 * time.Second}
	return step, appContext.State, kubevirtClient, kubeClient
}

func expectStoppedWithoutJob(t *testing.T, state multistep.StateBag, kubevirtClient *kubevirtfake.Clientset, kubeClient *k8sfake.Clientset) {
	t.Helper()
	vm, err := kubevirtClient.KubevirtV1().VirtualMachines("packer").Get(context.Background(), "base-vm", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get the Virtual Machine: %v", err)
	}
	if vm.Status.PrintableStatus != kubevirtv1.VirtualMachineStatusStopped {
		t.Errorf("expected the Virtual Machine to be stopped, status is: '%s'", vm.Status.PrintableStatus)
	}
	for _, action := range kubeClient.Actions() {
		if action.Matches("create", "jobs") {
			t.Errorf("expected no job to be created")
		}
	}
	if err, _ := state.Get(string(common.PackerError)).(error); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestStepGeneralizeSkipsVirtSysprepButStillStops(t *testing.T) {
	step, state, kubevirtClient, kubeClient := newGeneralizeStep(t, vmctx.Linux)
	step.SkipVirtSysprep = true

	if action := step.Run(context.Background(), state); action != multistep.ActionContinue {
		t.Errorf("expected the step to continue, got action: %v", action)
	}
	expectStoppedWithoutJob(t, state, kubevirtClient, kubeClient)
}

func TestStepGeneralizeLeavesWindowsToSysprep(t *testing.T) {
	step, state, kubevirtClient, kubeClient := newGeneralizeStep(t, vmctx.Windows)

	if action := step.Run(context.Background(), state); action != multistep.ActionContinue {
		t.Errorf("expected the step to continue, got action: %v", action)
	}
	expectStoppedWithoutJob(t, state, kubevirtClient, kubeClient)
}

func TestStepGeneralizeKeepsTheUserOfTheBuild(t *testing.T) {
	for name, test := range map[string]struct{ userToKeep, expected string }{
		"ssh user":                  {userToKeep: "ubuntu", expected: "ubuntu"},
		"no user, the default user": {userToKeep: "", expected: "packer"},
	} {
		t.Run(name, func(t *testing.T) {
			step, state, _, kubeClient := newGeneralizeStep(t, vmctx.Linux)
			step.UserToKeep = test.userToKeep

			var command []string
			kubeClient.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
				command = action.(k8stesting.CreateAction).GetObject().(*batchv1.Job).Spec.Template.Spec.Containers[0].Command
				return true, nil, errors.New("halt the step once the job creation is reached")
			})

			step.Run(context.Background(), state)

			keep := slices.Index(command, "--keep-user-accounts")
			if keep < 0 || keep+1 >= len(command) || command[keep+1] != test.expected {
				t.Errorf("expected virt-sysprep to keep the user '%s', got: %v", test.expected, command)
			}
		})
	}
}

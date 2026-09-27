package steps

import (
	"context"
	"errors"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	"packer-plugin-kubevirt/builder/common"
	vmctx "packer-plugin-kubevirt/builder/common/vm"
	"testing"
	"time"
)

func TestStepExportVMRunsSysprepOnceVirtualMachineIsStopped(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
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

	var statusAtJobCreation kubevirtv1.VirtualMachinePrintableStatus
	kubeClient := k8sfake.NewSimpleClientset()
	kubeClient.PrependReactor("create", "jobs", func(k8stesting.Action) (bool, runtime.Object, error) {
		currentVm, err := vmClient.Get(context.Background(), vm.Name, metav1.GetOptions{})
		if err != nil {
			return true, nil, err
		}
		statusAtJobCreation = currentVm.Status.PrintableStatus
		return true, nil, errors.New("halt the step once the job creation is reached")
	})

	virtClient := kubecli.NewMockKubevirtClient(gomock.NewController(t))
	virtClient.EXPECT().VirtualMachine(vm.Namespace).Return(vmClient).AnyTimes()
	virtClient.EXPECT().BatchV1().Return(kubeClient.BatchV1()).AnyTimes()

	osFamily := vmctx.Linux
	appContext := &common.AppContext{State: new(multistep.BasicStateBag)}
	appContext.Put(common.PackerUi, packersdk.TestUi(t))
	appContext.Put(common.VirtualMachine, vm)
	appContext.Put(common.VirtualMachineOsFamily, &osFamily)

	step := &StepExportVM{VirtClient: virtClient, VmExportTimeOut: 5 * time.Second}
	action := step.Run(context.Background(), appContext.State)

	if action != multistep.ActionHalt {
		t.Fatalf("expected the step to halt on job creation, got action: %v", action)
	}
	if statusAtJobCreation != kubevirtv1.VirtualMachineStatusStopped {
		t.Fatalf("expected the 'libguestfs' job to be created once the Virtual Machine is stopped, status was: '%s'", statusAtJobCreation)
	}
}

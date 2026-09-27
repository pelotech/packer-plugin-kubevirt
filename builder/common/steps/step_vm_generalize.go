package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/k8s/generator"
	vmctx "packer-plugin-kubevirt/builder/common/vm"
	"time"
)

type StepGeneralize struct {
	Clients         *k8s.Clients
	SkipVirtSysprep bool
	VmExportTimeOut time.Duration
}

func (s *StepGeneralize) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	ui := appContext.GetPackerUi()
	vm := appContext.GetVirtualMachine()

	ui.Say(fmt.Sprintf("stopping Virtual Machine for export %s/%s...", vm.Namespace, vm.Name))
	err := s.Clients.Kubevirt.KubevirtV1().VirtualMachines(vm.Namespace).Stop(context.TODO(), vm.Name, &kubevirtv1.StopOptions{})
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to stop Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err))
	}

	err = k8s.WaitForVirtualMachineStopped(s.Clients.Kubevirt.KubevirtV1().VirtualMachines(vm.Namespace), vm.Name, s.VmExportTimeOut)
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to stop Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err))
	}

	osFamily := *appContext.GetVirtualMachineOSFamily()
	if vmctx.Linux == osFamily && !s.SkipVirtSysprep {
		ui.Say(fmt.Sprintf("generify-ing with 'virt-sysprep' Virtual Machine for export %s/%s...", vm.Namespace, vm.Name))

		pvcName := generator.BuildDataVolumeName(vm.Name, generator.SourceDataVolumeSuffix)
		job := generator.GenerateGuestFSJob(vm, pvcName)

		job, err = s.Clients.Kubernetes.BatchV1().Jobs(vm.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		if err != nil {
			return appContext.Halt(fmt.Errorf("failed to create 'libguestfs' Job for Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err))
		}

		err = k8s.WaitForJobCompletion(s.Clients.Kubernetes, ui, job, s.VmExportTimeOut)
		if err != nil {
			return appContext.Halt(fmt.Errorf("error with 'libguestfs' job %s/%s: %s", vm.Namespace, vm.Name, err))
		}
	}

	return multistep.ActionContinue
}

func (s *StepGeneralize) Cleanup(_ multistep.StateBag) {}

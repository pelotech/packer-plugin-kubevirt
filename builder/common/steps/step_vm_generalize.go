package steps

import (
	"cmp"
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
	OsFamily        vmctx.OsFamily
	SkipVirtSysprep bool
	// virt-sysprep removes the other user accounts
	UserToKeep      string
	VmExportTimeOut time.Duration
}

func (s *StepGeneralize) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	ui := appContext.GetPackerUi()
	vm := appContext.GetVirtualMachine()

	ui.Say(fmt.Sprintf("stopping Virtual Machine for export %s/%s...", vm.Namespace, vm.Name))
	err := s.Clients.Kubevirt.KubevirtV1().VirtualMachines(vm.Namespace).Stop(ctx, vm.Name, &kubevirtv1.StopOptions{})
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to stop Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err))
	}

	err = k8s.WaitForVirtualMachineStopped(ctx, s.Clients.Kubevirt.KubevirtV1().VirtualMachines(vm.Namespace), vm.Name, s.VmExportTimeOut)
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to stop Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err))
	}

	if vmctx.Linux == s.OsFamily && !s.SkipVirtSysprep {
		ui.Say(fmt.Sprintf("generify-ing with 'virt-sysprep' Virtual Machine for export %s/%s...", vm.Namespace, vm.Name))

		job := generator.GenerateGuestFSJob(vm, cmp.Or(s.UserToKeep, common.VirtualMachineUsername))

		job, err = s.Clients.Kubernetes.BatchV1().Jobs(vm.Namespace).Create(ctx, job, metav1.CreateOptions{})
		if err != nil {
			return appContext.Halt(fmt.Errorf("failed to create 'libguestfs' Job for Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err))
		}

		err = k8s.WaitForJobCompletion(ctx, s.Clients.Kubernetes, ui, job, s.VmExportTimeOut)
		if err != nil {
			return appContext.Halt(fmt.Errorf("error with 'libguestfs' job %s/%s: %s", vm.Namespace, vm.Name, err))
		}
	}

	return multistep.ActionContinue
}

func (s *StepGeneralize) Cleanup(_ multistep.StateBag) {}

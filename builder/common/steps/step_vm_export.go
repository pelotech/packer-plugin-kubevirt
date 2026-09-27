package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	kubevirtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/k8s/generator"
	vmctx "packer-plugin-kubevirt/builder/common/vm"
	"time"
)

const (
	ExportTokenHeader = "x-kubevirt-export-token"
	secretTokenLength = 20
)

type StepExportVM struct {
	Clients         *k8s.Clients
	VmExportTimeOut time.Duration
}

func (s *StepExportVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
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
	if vmctx.Linux == osFamily {
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

	ui.Say(fmt.Sprintf("creating Virtual Machine Export %s/%s...", vm.Namespace, vm.Name))

	export, err := s.createExport(vm)
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to create Virtual Machine Export %s/%s: %s", vm.Namespace, vm.Name, err))
	}
	appContext.Put(common.VirtualMachineExport, export)

	exportToken := common.GenerateRandomPassword(secretTokenLength)
	err = s.createTokenSecret(export, exportToken)
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to create Virtual Machine Export secret %s/%s: %s", vm.Namespace, vm.Name, err))
	}
	appContext.Put(common.VirtualMachineExportToken, exportToken)

	err = s.waitForExportReady(ui, export)
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to wait for Virtual Machine Export to be in a 'Ready' state %s/%s: %s", vm.Namespace, vm.Name, err))
	}

	ui.Say(fmt.Sprintf("export step has completed for Virtual Machine %s/%s", vm.Namespace, vm.Name))

	return multistep.ActionContinue
}

func (s *StepExportVM) createExport(vm *kubevirtv1.VirtualMachine) (*exportv1.VirtualMachineExport, error) {
	export := generator.GenerateVirtualMachineExport(vm)
	export, err := s.Clients.Kubevirt.ExportV1().VirtualMachineExports(vm.Namespace).Create(context.TODO(), export, metav1.CreateOptions{})
	if k8serrors.IsNotFound(err) {
		return nil, fmt.Errorf("the cluster does not serve '%s', which needs KubeVirt 1.9 or later: %w", exportv1.SchemeGroupVersion, err)
	}
	return export, err
}

func (s *StepExportVM) waitForExportReady(ui packer.Ui, export *exportv1.VirtualMachineExport) error {
	ctx, cancel := context.WithTimeout(context.TODO(), s.VmExportTimeOut)
	defer cancel()

	watcher, err := s.Clients.Kubevirt.ExportV1().VirtualMachineExports(export.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", export.Name).String(),
	})
	if err != nil {
		return fmt.Errorf("failed to get Virtual Machine Export state: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				if ctx.Err() != nil {
					return fmt.Errorf("timeout waiting for Virtual Machine Export to be ready")
				}
				return fmt.Errorf("watch closed before Virtual Machine Export was ready")
			}
			updatedExport, ok := event.Object.(*exportv1.VirtualMachineExport)
			if !ok || updatedExport.Status == nil {
				continue
			}
			ui.Message(fmt.Sprintf("phase '%s'", updatedExport.Status.Phase))
			if updatedExport.Status.Phase == exportv1.Ready {
				return nil
			}

		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for Virtual Machine Export to be ready")
		}
	}
}

func (s *StepExportVM) createTokenSecret(export *exportv1.VirtualMachineExport, token string) error {
	secret := generator.GenerateTokenSecret(export, token)
	_, err := s.Clients.Kubernetes.CoreV1().Secrets(export.Namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}

func (s *StepExportVM) Cleanup(_ multistep.StateBag) {
	// Cleaning up 'Virtual Machine Export' during the build would prevent any post-processor to download the export
}

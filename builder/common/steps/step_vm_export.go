package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kubevirtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1beta1"
	"kubevirt.io/client-go/kubecli"
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
	VirtClient      kubecli.KubevirtClient
	VmExportTimeOut time.Duration
}

func (s *StepExportVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	ui := appContext.GetPackerUi()
	vm := appContext.GetVirtualMachine()

	ui.Say(fmt.Sprintf("stopping Virtual Machine for export %s/%s...", vm.Namespace, vm.Name))
	err := s.VirtClient.VirtualMachine(vm.Namespace).Stop(context.TODO(), vm.Name, &kubevirtv1.StopOptions{})
	if err != nil {
		err := fmt.Errorf("failed to stop Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	osFamily := *appContext.GetVirtualMachineOSFamily()
	if vmctx.Linux == osFamily {
		ui.Say(fmt.Sprintf("generify-ing with 'virt-sysprep' Virtual Machine for export %s/%s...", vm.Namespace, vm.Name))

		pvcName := generator.BuildDataVolumeName(vm.Name, generator.SourceDataVolumeSuffix)
		job := generator.GenerateGuestFSJob(vm, pvcName)

		job, err = s.VirtClient.BatchV1().Jobs(vm.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
		if err != nil {
			err = fmt.Errorf("failed to create 'libguestfs' Job for Virtual Machine %s/%s: %s", vm.Namespace, vm.Name, err)
			appContext.Put(common.PackerError, err)
			ui.Error(err.Error())

			return multistep.ActionHalt
		}

		err = k8s.WaitForJobCompletion(s.VirtClient.BatchV1(), ui, job, 2*time.Minute)
		if err != nil {
			err := fmt.Errorf("error with 'libguestfs' job %s/%s: %s", vm.Namespace, vm.Name, err)
			appContext.Put(common.PackerError, err)
			ui.Error(err.Error())

			return multistep.ActionHalt
		}
	}

	ui.Say(fmt.Sprintf("creating Virtual Machine Export %s/%s...", vm.Namespace, vm.Name))

	export, err := s.createExport(ui, vm)
	if err != nil {
		err := fmt.Errorf("failed to create Virtual Machine Export %s/%s: %s", vm.Namespace, vm.Name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}
	appContext.Put(common.VirtualMachineExport, export)

	exportToken := common.GenerateRandomPassword(secretTokenLength)
	_, err = s.createTokenSecret(export, exportToken)
	if err != nil {
		err := fmt.Errorf("failed to create Virtual Machine Export secret %s/%s: %s", vm.Namespace, vm.Name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}
	appContext.Put(common.VirtualMachineExportToken, exportToken)

	err = s.waitForExportReady(ui, export)
	if err != nil {
		err := fmt.Errorf("failed to wait for Virtual Machine Export to be in a 'Ready' state %s/%s: %s", vm.Namespace, vm.Name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	ui.Say(fmt.Sprintf("export step has completed for Virtual Machine %s/%s", vm.Namespace, vm.Name))

	return multistep.ActionContinue
}

func (s *StepExportVM) createExport(ui packer.Ui, vm *kubevirtv1.VirtualMachine) (*exportv1.VirtualMachineExport, error) {
	export := generator.GenerateVirtualMachineExport(vm)

	_, err := s.VirtClient.VirtualMachineExport(vm.Namespace).Get(context.TODO(), export.Name, metav1.GetOptions{})
	if k8serrors.IsAlreadyExists(err) {
		err = common.AskForRecreation(ui, func() error {
			return s.VirtClient.VirtualMachineExport(vm.Namespace).Delete(context.TODO(), export.Name, metav1.DeleteOptions{})
		})
		if err != nil {
			return nil, err
		}
	}

	export, err = s.VirtClient.VirtualMachineExport(vm.Namespace).Create(context.TODO(), export, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return export, nil
}

func (s *StepExportVM) waitForExportReady(ui packer.Ui, export *exportv1.VirtualMachineExport) error {
	ctx, cancel := context.WithTimeout(context.TODO(), s.VmExportTimeOut)
	defer cancel()

	watcher, _ := s.VirtClient.VirtualMachineExport(export.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: labels.SelectorFromSet(map[string]string{
			"metadata.name": export.Name,
		}).String(),
	})
	defer watcher.Stop()

	for {
		select {
		case event, _ := <-watcher.ResultChan():
			updatedExport, _ := event.Object.(*exportv1.VirtualMachineExport)
			ui.Message(fmt.Sprintf("phase '%s'", updatedExport.Status.Phase))
			if updatedExport.Status.Phase == exportv1.Ready {
				return nil
			}

		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for Virtual Machine Export to be ready")
		}
	}
}

func (s *StepExportVM) createTokenSecret(export *exportv1.VirtualMachineExport, token string) (*corev1.Secret, error) {
	secret := generator.GenerateTokenSecret(export, token)
	secret, err := s.VirtClient.CoreV1().Secrets(export.Namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return nil, err
	}

	return secret, nil
}

func (s *StepExportVM) Cleanup(_ multistep.StateBag) {
	// Cleaning up 'Virtual Machine Export' during the build would prevent any post-processor to download the export
}

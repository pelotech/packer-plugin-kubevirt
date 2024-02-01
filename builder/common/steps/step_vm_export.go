package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	kubevirtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1alpha1"
	"kubevirt.io/client-go/kubecli"
	"log"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	vmgenerator "packer-plugin-kubevirt/builder/common/k8s/generator"
	"packer-plugin-kubevirt/builder/common/utils"
	"time"
)

const (
	ExportTokenHeader = "x-kubevirt-export-token"
	secretTokenLength = 20
)

type StepExportVM struct {
	VirtClient kubecli.KubevirtClient
}

func (s *StepExportVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	ui := appContext.GetPackerUi()
	vm := appContext.GetVirtualMachine()

	token := utils.GenerateRandomPassword(secretTokenLength)
	appContext.Put(common.VirtualMachineExportToken, token)

	export, err := s.createExport(vm, token)
	if err != nil {
		err := fmt.Errorf("failed to create Virtual Machine Export %s/%s: %s", vm.Namespace, vm.Name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	err = s.waitForExportReady(vm.Namespace, vm.Name)
	if err != nil {
		err := fmt.Errorf("failed to wait for Virtual Machine Export to be in a 'Ready' state %s/%s: %s", vm.Namespace, vm.Name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	appContext.Put(common.VirtualMachineExport, export)

	return multistep.ActionContinue
}

func (s *StepExportVM) createExport(vm *kubevirtv1.VirtualMachine, token string) (*exportv1.VirtualMachineExport, error) {
	export := vmgenerator.GenerateVirtualMachineExport(vm)
	result, err := s.VirtClient.VirtualMachineExport(vm.Namespace).Create(context.TODO(), export, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	_, err = s.getOrCreateTokenSecret(export, token)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *StepExportVM) waitForExportReady(ns, name string) error {
	watchFunc := func(event watch.Event) (bool, error) {
		export, err := event.Object.(*exportv1.VirtualMachineExport)
		if !err {
			return false, fmt.Errorf("unexpected type for %v", event.Object)
		}

		switch export.Status.Phase {
		case exportv1.Ready:
			log.Printf("Virtual Machine Export '%s' is now ready", name)
			return true, nil
		case exportv1.Skipped, exportv1.Terminated:
			return false, fmt.Errorf("virtual Machine Export '%s' failed", name)
		}

		return false, nil
	}

	_, err := k8s.WaitForResource(s.VirtClient.RestClient(), k8s.VirtualMachineGroupVersionResource, ns, name, 5*time.Minute, watchFunc)
	if err != nil {
		return fmt.Errorf("failed to wait for Virtual Machine Export %s/%s to be ready: %s", ns, name, err)
	}

	return nil

}

func (s *StepExportVM) getOrCreateTokenSecret(export *exportv1.VirtualMachineExport, token string) (*corev1.Secret, error) {
	secret := vmgenerator.GenerateTokenSecret(export, token)
	secret, err := s.VirtClient.CoreV1().Secrets(export.Namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return nil, err
	}

	return secret, nil
}

func (s *StepExportVM) Cleanup(_ multistep.StateBag) {
	// No action to be taken, cascade deletion from VM to VM export, export secret deletion.
}

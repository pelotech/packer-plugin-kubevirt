package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	kubevirtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/k8s/generator"
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

	ui.Say(fmt.Sprintf("creating Virtual Machine Export %s/%s...", vm.Namespace, vm.Name))

	export, err := s.createExport(vm)
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to create Virtual Machine Export %s/%s: %s", vm.Namespace, vm.Name, err))
	}
	appContext.Put(common.VirtualMachineExport, export)

	err = s.handOverVirtualMachine(vm, export)
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to hand Virtual Machine %s/%s over to its export: %s", vm.Namespace, vm.Name, err))
	}
	appContext.Put(common.VirtualMachineOwnedByExport, true)

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

// handOverVirtualMachine makes the export the owner of the stopped Virtual Machine, so the disk lives as long as the export
func (s *StepExportVM) handOverVirtualMachine(vm *kubevirtv1.VirtualMachine, export *exportv1.VirtualMachineExport) error {
	vms := s.Clients.Kubevirt.KubevirtV1().VirtualMachines(vm.Namespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := vms.Get(context.TODO(), vm.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		patch, err := json.Marshal(map[string]interface{}{
			"metadata": map[string]interface{}{
				"resourceVersion": current.ResourceVersion,
				"ownerReferences": append(current.OwnerReferences, generator.GenerateExportOwnerReference(export)),
			},
		})
		if err != nil {
			return err
		}
		_, err = vms.Patch(context.TODO(), vm.Name, types.MergePatchType, patch, metav1.PatchOptions{})
		return err
	})
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

// Cleanup deletes the export of a failed build only: the post-processors download from it
func (s *StepExportVM) Cleanup(state multistep.StateBag) {
	appContext := &common.AppContext{State: state}
	export := appContext.GetVirtualMachineExport()
	if export == nil || !appContext.BuildFailed() {
		return
	}

	err := s.Clients.Kubevirt.ExportV1().VirtualMachineExports(export.Namespace).Delete(context.TODO(), export.Name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		appContext.GetPackerUi().Error(fmt.Sprintf("failed to delete Virtual Machine Export %s/%s: %s", export.Namespace, export.Name, err))
		return
	}
	appContext.GetPackerUi().Message(fmt.Sprintf("Virtual Machine Export %s/%s has been deleted", export.Namespace, export.Name))
}

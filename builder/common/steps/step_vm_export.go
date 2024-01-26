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
	"packer-plugin-kubevirt/builder/common/utils"
	"time"
)

const (
	ExportTokenHeader = "x-kubevirt-export-token"
	secretTokenKey    = "token"
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

	appContext.Put(common.VirtualMachineExport, &export)

	return multistep.ActionContinue
}

func (s *StepExportVM) createExport(vm kubevirtv1.VirtualMachine, token string) (*exportv1.VirtualMachineExport, error) {
	exportSource := corev1.TypedLocalObjectReference{
		APIGroup: &kubevirtv1.VirtualMachineGroupVersionKind.Group,
		Kind:     kubevirtv1.VirtualMachineGroupVersionKind.Kind,
		Name:     vm.Name,
	}
	exportTokenSecret := fmt.Sprintf("export-token-%s", vm.Name)
	export := &exportv1.VirtualMachineExport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vm.Name,
			Namespace: vm.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(&vm, k8s.VirtualMachineGroupVersionKind),
			},
		},
		Spec: exportv1.VirtualMachineExportSpec{
			TokenSecretRef: &exportTokenSecret,
			Source:         exportSource,
		},
	}
	result, err := s.VirtClient.VirtualMachineExport(vm.Namespace).Create(context.TODO(), export, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	// Create secret after export to build 'OwnerReference' relationship 'VME to secret'
	_, err = getOrCreateTokenSecret(s.VirtClient, export, token)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *StepExportVM) waitForExportReady(ns, name string) error {
	watchFunc := func(ctx context.Context, event watch.Event) (bool, error) {
		export, err := event.Object.(*exportv1.VirtualMachineExport)
		if !err {
			return false, fmt.Errorf("unexpected type for %v", event.Object)
		}
		switch export.Status.Phase {
		case exportv1.Pending:
			select {
			case <-ctx.Done():
				return false, fmt.Errorf("timeout waiting for Virtual Machine Export '%s'", name)
			case <-time.After(5 * time.Second):
				// Continue waiting
			}
		case exportv1.Ready:
			log.Printf("Virtual Machine Export '%s' is now ready", name)
			return true, nil
		case exportv1.Skipped, exportv1.Terminated:
			return false, fmt.Errorf("virtual Machine Export '%s' failed", name)
		}

		return false, nil
	}
	return k8s.WaitForResource(s.VirtClient.DynamicClient(), k8s.VirtualMachineGroupVersionResource, ns, name, 5*time.Minute, watchFunc)

}

func getOrCreateTokenSecret(client kubecli.KubevirtClient, vmexport *exportv1.VirtualMachineExport, token string) (*corev1.Secret, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      *vmexport.Spec.TokenSecretRef,
			Namespace: vmexport.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(vmexport, k8s.VirtualMachineExportGroupVersionResource),
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			secretTokenKey: token,
		},
	}

	secret, err := client.CoreV1().Secrets(vmexport.Namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return nil, err
	}

	return secret, nil
}

func (s *StepExportVM) Cleanup(_ multistep.StateBag) {
	// No action to be taken, cascade deletion from VM to VM export, export secret deletion.
}

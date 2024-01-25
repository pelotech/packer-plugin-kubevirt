package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	kubevirtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1alpha1"
	"kubevirt.io/client-go/kubecli"
	"log"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/utils"
	"time"
)

const (
	secretTokenKey    = "token"
	secretTokenLength = 20
)

type StepExportVM struct {
	VirtClient kubecli.KubevirtClient
}

func (s *StepExportVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)
	ns := state.Get("namespace").(string)
	name := state.Get("name").(string)

	token := utils.GenerateRandomPassword(secretTokenLength)

	export, err := s.createExport(ns, name, token)
	if err != nil {
		err := fmt.Errorf("failed to create Virtual Machine Export %s/%s: %s", ns, name, err)
		state.Put("error", err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	err = s.waitForExportReady(ns, name)
	if err != nil {
		err := fmt.Errorf("failed to wait for Virtual Machine Export to be in a 'Ready' state %s/%s: %s", ns, name, err)
		state.Put("error", err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *StepExportVM) createExport(ns, name, token string) (*exportv1.VirtualMachineExport, error) {
	exportSource := corev1.TypedLocalObjectReference{
		APIGroup: &kubevirtv1.VirtualMachineGroupVersionKind.Group,
		Kind:     kubevirtv1.VirtualMachineGroupVersionKind.Kind,
		Name:     name,
	}
	exportTokenSecret := fmt.Sprintf("export-token-%s", name)
	export := &exportv1.VirtualMachineExport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			// TODO: expose VM object from previous step or resolve by string if no clean alternative
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(vm, k8s.VirtualMachineGroupVersionKind),
			},
		},
		Spec: exportv1.VirtualMachineExportSpec{
			TokenSecretRef: &exportTokenSecret,
			Source:         exportSource,
		},
	}
	result, err := s.VirtClient.VirtualMachineExport(ns).Create(context.TODO(), export, metav1.CreateOptions{})
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

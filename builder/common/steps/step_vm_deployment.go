package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
	"packer-plugin-kubevirt/builder/common/k8s"
	vmgenerator "packer-plugin-kubevirt/builder/common/k8s/vm-generator"
	"time"
)

type StepDeployVM struct {
	VirtClient kubecli.KubevirtClient
	VmOptions  vmgenerator.VirtualMachineOptions
}

func (s *StepDeployVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)
	ns := state.Get("namespace").(string)
	name := state.Get("name").(string)

	vm := vmgenerator.GenerateVirtualMachine(s.VmOptions)
	vm, err := s.VirtClient.VirtualMachine(ns).Create(context.TODO(), vm)
	if err != nil {
		ui.Error(fmt.Sprintf("Failed to provision Virtual Machine %s/%s: %s", ns, name, err))
		return multistep.ActionHalt
	}

	if s.VmOptions.Credentials != nil {
		vmgenerator.GenerateCredentials(vm, s.VmOptions)
	}

	if s.VmOptions.StartupScriptSecretName != "" {
		startupScriptSecret := vmgenerator.GenerateStartupScriptSecret(vm, s.VmOptions)
		_, err = s.VirtClient.CoreV1().Secrets(ns).Create(context.TODO(), startupScriptSecret, metav1.CreateOptions{})
		if err != nil {
			ui.Error(fmt.Sprintf("Failed to provision Virtual Machine startup script secret %s/%s: %s", ns, name, err))
			return multistep.ActionHalt
		}
	}

	_ = s.waitForVirtualMachine(ns, name)
	ui.Say(fmt.Sprintf("VM has been started, initialized and is ready to be provisioned: %s/%s", ns, name))

	return multistep.ActionContinue
}

func (s *StepDeployVM) waitForVirtualMachine(ns, name string) error {
	watchFunc := func(ctx context.Context, event watch.Event) (bool, error) {
		vm, ok := event.Object.(*kubevirtv1.VirtualMachine)
		if !ok {
			return false, fmt.Errorf("unexpected type for %v", event.Object)
		}

		for _, condition := range vm.Status.Conditions {
			if condition.Type == kubevirtv1.VirtualMachineReady && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}
	return k8s.WaitForResource(s.VirtClient.DynamicClient(), k8s.VirtualMachineGroupVersionResource, ns, name, 5*time.Minute, watchFunc)
}

func (s *StepDeployVM) Cleanup(state multistep.StateBag) {
	namespace := state.Get("namespace").(string)
	name := state.Get("name").(string)
	deletionPropagation := metav1.DeletePropagationForeground

	_ = s.VirtClient.VirtualMachine(namespace).Delete(context.TODO(), name, &metav1.DeleteOptions{PropagationPolicy: &deletionPropagation})
}

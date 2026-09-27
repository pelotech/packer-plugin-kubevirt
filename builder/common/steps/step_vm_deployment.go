package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/k8s/generator"
)

type StepDeployVM struct {
	Clients   *k8s.Clients
	VmOptions generator.VirtualMachineOptions
}

func (s *StepDeployVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	ui := appContext.GetPackerUi()
	ns := s.VmOptions.Namespace
	name := s.VmOptions.Name

	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
	_, err := s.Clients.Kubernetes.CoreV1().Namespaces().Create(context.TODO(), namespace, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return appContext.Halt(fmt.Errorf("failed to create namespace for Virtual Machine %s/%s: %s", ns, name, err))
	}

	ui.Say(fmt.Sprintf("creating Virtual Machine %s/%s...", ns, name))
	vm := generator.GenerateVirtualMachine(s.VmOptions)
	vm, err = s.Clients.Kubevirt.KubevirtV1().VirtualMachines(ns).Create(context.TODO(), vm, metav1.CreateOptions{})
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to create Virtual Machine %s/%s: %s", ns, name, err))
	}
	appContext.Put(common.VirtualMachine, vm)

	if s.VmOptions.ImageSource.HasS3Credentials() {
		s3CredentialsSecret := generator.GenerateS3CredentialsSecret(vm, s.VmOptions)
		_, err = s.Clients.Kubernetes.CoreV1().Secrets(ns).Create(context.TODO(), s3CredentialsSecret, metav1.CreateOptions{})
		if err != nil {
			return appContext.Halt(fmt.Errorf("failed to create s3 credentials secret for Virtual Machine %s/%s: %s", ns, name, err))
		}
	}

	startupScriptSecret, err := generator.GenerateStartupScriptSecret(vm, s.VmOptions)
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to generate startup script secret spec for Virtual Machine %s/%s: %s", ns, name, err))
	}
	_, err = s.Clients.Kubernetes.CoreV1().Secrets(ns).Create(context.TODO(), startupScriptSecret, metav1.CreateOptions{})
	if err != nil {
		return appContext.Halt(fmt.Errorf("failed to create startup script secret for Virtual Machine %s/%s: %s", ns, name, err))
	}

	if s.VmOptions.Credentials != nil {
		userCredentialsSecret := generator.GenerateUserCredentialsSecret(vm, s.VmOptions)
		_, err = s.Clients.Kubernetes.CoreV1().Secrets(ns).Create(context.TODO(), userCredentialsSecret, metav1.CreateOptions{})
		if err != nil {
			return appContext.Halt(fmt.Errorf("failed to create user credentials secret for Virtual Machine %s/%s: %s", ns, name, err))
		}
	}

	return multistep.ActionContinue
}

// Cleanup doesn't delete the node pool and namespace, it may contain other resources that are not created by this build context
func (s *StepDeployVM) Cleanup(state multistep.StateBag) {
	appContext := &common.AppContext{State: state}
	vm := appContext.GetVirtualMachine()
	if vm == nil {
		return
	}

	propagationPolicy := metav1.DeletePropagationForeground
	_ = s.Clients.Kubevirt.KubevirtV1().VirtualMachines(vm.Namespace).Delete(context.TODO(), vm.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	appContext.GetPackerUi().Message(fmt.Sprintf("Virtual Machine %s/%s has been deleted", vm.Namespace, vm.Name))
}

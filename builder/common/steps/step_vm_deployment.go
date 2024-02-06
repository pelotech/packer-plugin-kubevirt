package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/k8s/generator"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

type StepDeployVM struct {
	KubeClient               client.Client
	VirtClient               kubecli.KubevirtClient
	KubernetesNodeAutoscaler k8s.NodeAutoscaler
	VmOptions                generator.VirtualMachineOptions
	VmDeploymentTimeOut      time.Duration
}

func (s *StepDeployVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	appContext := &common.AppContext{State: state}
	ui := appContext.GetPackerUi()
	ns := s.VmOptions.Namespace
	name := s.VmOptions.Name

	err := s.bootstrapEnvironment(ns, name)
	if err != nil {
		err := fmt.Errorf("failed to bootstrap environment for Virtual Machine %s/%s: %s", ns, name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	ui.Say(fmt.Sprintf("creating Virtual Machine %s/%s...", ns, name))
	vm := generator.GenerateVirtualMachine(s.VmOptions)
	vm, err = s.VirtClient.VirtualMachine(ns).Create(context.TODO(), vm)
	if err != nil {
		err := fmt.Errorf("failed to create Virtual Machine %s/%s: %s", ns, name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}
	appContext.Put(common.VirtualMachine, vm)

	if s.VmOptions.ImageSource.AWSAccessKeyId != "" && s.VmOptions.ImageSource.AWSSecretAccessKey != "" {
		s3CredentialsSecret := generator.GenerateS3CredentialsSecret(vm, s.VmOptions)
		_, err = s.VirtClient.CoreV1().Secrets(ns).Create(context.TODO(), s3CredentialsSecret, metav1.CreateOptions{})
		if err != nil {
			err := fmt.Errorf("failed to create s3 credentials secret for Virtual Machine %s/%s: %s", ns, name, err)
			appContext.Put(common.PackerError, err)
			ui.Error(err.Error())

			return multistep.ActionHalt
		}
	}

	startupScriptSecret, err := generator.GenerateStartupScriptSecret(vm, s.VmOptions)
	if err != nil {
		err := fmt.Errorf("failed to generate startup script secret spec for Virtual Machine %s/%s: %s", ns, name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}
	_, err = s.VirtClient.CoreV1().Secrets(ns).Create(context.TODO(), startupScriptSecret, metav1.CreateOptions{})
	if err != nil {
		err := fmt.Errorf("failed to create startup script secret for Virtual Machine %s/%s: %s", ns, name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	if s.VmOptions.Credentials != nil {
		userCredentialsSecret := generator.GenerateUserCredentialsSecret(vm, s.VmOptions)
		_, err = s.VirtClient.CoreV1().Secrets(ns).Create(context.TODO(), userCredentialsSecret, metav1.CreateOptions{})
		if err != nil {
			err := fmt.Errorf("failed to create user credentials secret for Virtual Machine %s/%s: %s", ns, name, err)
			appContext.Put(common.PackerError, err)
			ui.Error(err.Error())

			return multistep.ActionHalt
		}
	}

	err = s.waitForVirtualMachine(ui, vm)
	if err != nil {
		err = fmt.Errorf("failed to wait to be in a 'Ready' state for Virtual Machine %s/%s: %s", ns, name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	ui.Say(fmt.Sprintf("deployment step has completed for Virtual Machine %s/%s", ns, name))

	return multistep.ActionContinue
}

func (s *StepDeployVM) waitForVirtualMachine(ui packer.Ui, vm *kubevirtv1.VirtualMachine) error {
	watchFunc := func(event watch.Event) (bool, error) {
		vm, ok := event.Object.(*kubevirtv1.VirtualMachine)
		if !ok {
			return false, fmt.Errorf("unexpected type for %v", event.Object)
		}

		for index, condition := range vm.Status.Conditions {
			if index == 0 {
				ui.Message(fmt.Sprintf("condition '%s' changed to '%s'", condition.Type, condition.Status))
			}
			if condition.Type == kubevirtv1.VirtualMachineReady && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}
	_, err := k8s.WaitForResource(s.VirtClient.RestClient(), vm.Namespace, k8s.VirtualMachineResourceName, vm.Name, vm.ResourceVersion, s.VmDeploymentTimeOut, watchFunc)
	if err != nil {
		return fmt.Errorf("failed to wait for Virtual Machine %s/%s to be ready: %s", vm.Namespace, vm.Name, err)
	}

	return nil
}

func (s *StepDeployVM) bootstrapEnvironment(ns, name string) error {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
	_, err := s.VirtClient.CoreV1().Namespaces().Create(context.TODO(), namespace, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	switch s.KubernetesNodeAutoscaler {
	case k8s.KarpenterNodeAutoscaler:
		nodePool := generator.GenerateNodePool()
		err = s.KubeClient.Create(context.TODO(), nodePool)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}

		job := generator.GenerateInitJob(ns, name, 2*time.Minute, k8s.KarpenterNodeAutoscaler)
		_, err = s.VirtClient.BatchV1().Jobs(ns).Create(context.TODO(), job, metav1.CreateOptions{})
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	case k8s.DefaultNodeAutoscaler:
		// Do nothing
	}

	return nil
}

// Cleanup doesn't delete the node pool and namespace, it may contain other resources that are not created by this build context
func (s *StepDeployVM) Cleanup(state multistep.StateBag) {
	appContext := &common.AppContext{State: state}
	vm := appContext.GetVirtualMachine()
	if appContext.GetVirtualMachine() == nil {
		return
	}

	propagationPolicy := metav1.DeletePropagationForeground
	_ = s.VirtClient.VirtualMachine(vm.Namespace).Delete(context.TODO(), vm.Name, &metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
	appContext.GetPackerUi().Message(fmt.Sprintf("Virtual Machine %s/%s has been deleted", vm.Namespace, vm.Name))
}

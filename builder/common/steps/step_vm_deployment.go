package steps

import (
	"context"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/k8s/generator"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

type StepDeployVM struct {
	KubeClient           client.Client
	VirtClient           kubecli.KubevirtClient
	VmOptions            generator.VirtualMachineOptions
	UseKarpenterNodePool bool
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

	err = waitForVirtualMachine(s.VirtClient.RestClient(), ns, name)
	if err != nil {
		err = fmt.Errorf("failed to wait to be in a 'Ready' state for Virtual Machine %s/%s: %s", ns, name, err)
		appContext.Put(common.PackerError, err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	ui.Say(fmt.Sprintf("deployment step has completed for Virtual Machine %s/%s", ns, name))

	return multistep.ActionContinue
}

func waitForVirtualMachine(client *rest.RESTClient, ns, name string) error {
	watchFunc := func(event watch.Event) (bool, error) {
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

	_, err := k8s.WaitForResource(client, k8s.VirtualMachineGroupVersionResource, ns, name, 8*time.Minute, watchFunc)
	if err != nil {
		return fmt.Errorf("failed to wait for Virtual Machine %s/%s to be ready: %s", ns, name, err)
	}

	return nil
}

func (s *StepDeployVM) bootstrapEnvironment(ns, name string) error {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
	_, err := s.VirtClient.CoreV1().Namespaces().Create(context.TODO(), namespace, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	if s.UseKarpenterNodePool {
		nodePool := generator.GenerateNodePool()
		err = s.KubeClient.Create(context.TODO(), nodePool)
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}

		ttlInSeconds := 120
		pod := generator.GenerateInitPod(ns, name, ttlInSeconds)
		_, err = s.VirtClient.CoreV1().Pods(ns).Create(context.TODO(), pod, metav1.CreateOptions{})
		if err != nil && !errors.IsAlreadyExists(err) {
			return err
		}
	}

	return nil
}

func (s *StepDeployVM) Cleanup(state multistep.StateBag) {
	appContext := &common.AppContext{State: state}
	name := appContext.GetVirtualMachine().Name
	namespace := appContext.GetVirtualMachine().Namespace
	deletionPropagation := metav1.DeletePropagationForeground
	// NOTE: We don't delete the node pool and namespace here because it may contain other resources that are not created by this build context.
	_ = s.VirtClient.VirtualMachine(namespace).Delete(context.TODO(), name, &metav1.DeleteOptions{PropagationPolicy: &deletionPropagation})
	appContext.GetPackerUi().Message(fmt.Sprintf("clean up - Virtual Machine %s/%s has been deleted", namespace, name))
}

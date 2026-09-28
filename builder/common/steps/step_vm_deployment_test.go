package steps

import (
	"context"
	"errors"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	kubevirtv1 "kubevirt.io/api/core/v1"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/k8s/generator"
	vmctx "packer-plugin-kubevirt/builder/common/vm"
	"testing"
)

func TestStepDeployVMCleanup(t *testing.T) {
	for name, test := range map[string]struct {
		ownedByExport bool
		failure       string
		keepVm        bool
	}{
		"owned by its export, successful build": {ownedByExport: true, keepVm: true},
		"owned by its export, halted build":     {ownedByExport: true, failure: multistep.StateHalted},
		"owned by its export, cancelled build":  {ownedByExport: true, failure: multistep.StateCancelled},
		"not handed over":                       {ownedByExport: false},
	} {
		t.Run(name, func(t *testing.T) {
			vm := &kubevirtv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"}}
			kubevirtClient := kubevirtfake.NewSimpleClientset(vm)
			appContext := &common.AppContext{State: new(multistep.BasicStateBag)}
			appContext.Put(common.PackerUi, packersdk.TestUi(t))
			appContext.Put(common.VirtualMachine, vm)
			if test.ownedByExport {
				appContext.Put(common.VirtualMachineOwnedByExport, true)
			}
			if test.failure != "" {
				appContext.State.Put(test.failure, true)
			}

			step := &StepDeployVM{Clients: &k8s.Clients{Kubevirt: kubevirtClient}}
			step.Cleanup(appContext.State)

			_, err := kubevirtClient.KubevirtV1().VirtualMachines(vm.Namespace).Get(context.Background(), vm.Name, metav1.GetOptions{})
			if kept := err == nil; kept != test.keepVm {
				t.Errorf("expected the Virtual Machine to be kept: %t, got: %t (%v)", test.keepVm, kept, err)
			}
		})
	}
}

// newDeployStep gives a step whose namespace creation fails with the given error
func newDeployStep(t *testing.T, namespaceError error) (*StepDeployVM, multistep.StateBag, *kubevirtfake.Clientset) {
	t.Helper()
	kubeClient := k8sfake.NewSimpleClientset()
	kubeClient.PrependReactor("create", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, namespaceError
	})
	kubevirtClient := kubevirtfake.NewSimpleClientset()

	appContext := &common.AppContext{State: new(multistep.BasicStateBag)}
	appContext.Put(common.PackerUi, packersdk.TestUi(t))

	step := &StepDeployVM{
		Clients: &k8s.Clients{Kubernetes: kubeClient, Kubevirt: kubevirtClient},
		VmOptions: generator.VirtualMachineOptions{
			Name:      "base-ubuntu",
			Namespace: "packer",
			OsFamily:  vmctx.Linux,
			DiskSize:  "10Gi",
			CPU:       "2",
			Memory:    "4Gi",
		},
	}
	return step, appContext.State, kubevirtClient
}

func TestStepDeployVMUsesTheNamespaceWhenNotAllowedToCreateNamespaces(t *testing.T) {
	forbidden := k8serrors.NewForbidden(corev1.Resource("namespaces"), "", errors.New(`User "system:serviceaccount:packer:builder" cannot create resource "namespaces" in API group "" at the cluster scope`))
	step, state, kubevirtClient := newDeployStep(t, forbidden)

	if action := step.Run(context.Background(), state); action != multistep.ActionContinue {
		t.Fatalf("expected the step to continue, got action: %v (%v)", action, state.Get(string(common.PackerError)))
	}
	if _, err := kubevirtClient.KubevirtV1().VirtualMachines("packer").Get(context.Background(), "base-ubuntu", metav1.GetOptions{}); err != nil {
		t.Errorf("expected the Virtual Machine to be created: %v", err)
	}
}

func TestStepDeployVMHaltsWhenTheNamespaceCreationFails(t *testing.T) {
	step, state, kubevirtClient := newDeployStep(t, k8serrors.NewInternalError(errors.New("etcdserver: request timed out")))

	if action := step.Run(context.Background(), state); action != multistep.ActionHalt {
		t.Fatalf("expected the step to halt, got action: %v", action)
	}
	if _, err := kubevirtClient.KubevirtV1().VirtualMachines("packer").Get(context.Background(), "base-ubuntu", metav1.GetOptions{}); !k8serrors.IsNotFound(err) {
		t.Errorf("expected no Virtual Machine to be created, got: %v", err)
	}
}

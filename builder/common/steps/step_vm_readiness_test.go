package steps

import (
	"context"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"testing"
	"time"
)

func TestStepWaitForVMReadsTheCurrentState(t *testing.T) {
	created := &kubevirtv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Name: "base-windows", Namespace: "packer"}}
	current := created.DeepCopy()
	current.Status.Conditions = []kubevirtv1.VirtualMachineCondition{
		{Type: kubevirtv1.VirtualMachineReady, Status: corev1.ConditionTrue},
	}

	appContext := &common.AppContext{State: new(multistep.BasicStateBag)}
	appContext.Put(common.PackerUi, packersdk.TestUi(t))
	appContext.Put(common.VirtualMachine, created)

	// the Virtual Machine got ready while the boot command was typed, there is no event left to wait for
	step := &StepWaitForVM{Clients: &k8s.Clients{Kubevirt: kubevirtfake.NewSimpleClientset(current)}, VmDeploymentTimeOut: time.Second}
	if action := step.Run(context.Background(), appContext.State); action != multistep.ActionContinue {
		t.Fatalf("expected the step to continue, got action: %v, error: %v", action, appContext.GetPackerError())
	}
}

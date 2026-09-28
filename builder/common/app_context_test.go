package common_test

import (
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s/generator"
	"testing"
)

func TestBuildArtifactFromGeneratedExport(t *testing.T) {
	vm := &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
	}
	appContext := &common.AppContext{State: new(multistep.BasicStateBag)}
	appContext.Put(common.VirtualMachineExport, generator.GenerateVirtualMachineExport(vm, 0))
	appContext.Put(common.VirtualMachineExportToken, "token")

	artifact := appContext.BuildArtifact("kubevirt", "ubuntu", "10Gi")

	if name := artifact.State(common.VirtualMachineExportNameArtifactKey); name != vm.Name {
		t.Errorf("expected export name '%s', got: '%v'", vm.Name, name)
	}
	if namespace := artifact.State(common.NamespaceArtifactKey); namespace != vm.Namespace {
		t.Errorf("expected export namespace '%s', got: '%v'", vm.Namespace, namespace)
	}
	if preference := artifact.State(common.PreferenceArtifactKey); preference != "ubuntu" {
		t.Errorf("expected preference 'ubuntu', got: '%v'", preference)
	}
	if diskSize := artifact.State(common.DiskSizeArtifactKey); diskSize != "10Gi" {
		t.Errorf("expected disk size '10Gi', got: '%v'", diskSize)
	}
}

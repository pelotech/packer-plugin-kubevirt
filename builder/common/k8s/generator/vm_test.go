package generator

import (
	corev1 "k8s.io/api/core/v1"
	"packer-plugin-kubevirt/builder/common/vm"
	"testing"
)

func TestGenerateVirtualMachineResources(t *testing.T) {
	virtualMachine := GenerateVirtualMachine(VirtualMachineOptions{
		Name:      "base-ubuntu",
		Namespace: "packer",
		OsFamily:  vm.Linux,
		DiskSpace: "10Gi",
		CPU:       "2",
		Memory:    "4Gi",
	})

	requests := virtualMachine.Spec.Template.Spec.Domain.Resources.Requests
	if cpu := requests[corev1.ResourceCPU]; cpu.String() != "2" {
		t.Errorf("expected 2 CPUs, got: %s", cpu.String())
	}
	if memory := requests[corev1.ResourceMemory]; memory.String() != "4Gi" {
		t.Errorf("expected 4Gi of memory, got: %s", memory.String())
	}
}

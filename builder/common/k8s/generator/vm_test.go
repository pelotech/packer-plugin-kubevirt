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

func TestGenerateStartupScriptSecretForWindows(t *testing.T) {
	virtualMachine := GenerateVirtualMachine(VirtualMachineOptions{
		Name:      "base-windows",
		Namespace: "packer",
		OsFamily:  vm.Windows,
		DiskSpace: "15Gi",
		CPU:       "2",
		Memory:    "4Gi",
	})
	defaultSysprep, err := scripts.ReadFile("scripts/autounattend.xml")
	if err != nil {
		t.Fatalf("failed to read the default answer file: %v", err)
	}

	for name, test := range map[string]struct {
		sysprep  string
		expected string
	}{
		"custom answer file":  {sysprep: "<unattend>custom</unattend>", expected: "<unattend>custom</unattend>"},
		"default answer file": {sysprep: "", expected: string(defaultSysprep)},
	} {
		t.Run(name, func(t *testing.T) {
			secret, err := GenerateStartupScriptSecret(virtualMachine, VirtualMachineOptions{
				Name:             "base-windows",
				Namespace:        "packer",
				OsDistribution:   "windows.10.virtio",
				OsFamily:         vm.Windows,
				UserProvisioning: UserProvisioning{Sysprep: test.sysprep},
			})
			if err != nil {
				t.Fatalf("failed to generate the startup script secret: %v", err)
			}
			if sysprep := secret.StringData["autounattend.xml"]; sysprep != test.expected {
				t.Errorf("expected answer file '%.40s', got: '%.40s'", test.expected, sysprep)
			}
		})
	}
}

package generator

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"os"
	"os/exec"
	"packer-plugin-kubevirt/builder/common/vm"
	"path/filepath"
	"runtime"
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

func TestGenerateVirtualMachineRunStrategy(t *testing.T) {
	virtualMachine := GenerateVirtualMachine(VirtualMachineOptions{
		Name:      "base-ubuntu",
		Namespace: "packer",
		OsFamily:  vm.Linux,
		DiskSpace: "10Gi",
		CPU:       "2",
		Memory:    "4Gi",
	})

	if virtualMachine.Spec.Running != nil {
		t.Errorf("expected the deprecated 'running' field to be left out, got: %v", *virtualMachine.Spec.Running)
	}
	if strategy := virtualMachine.Spec.RunStrategy; strategy == nil || *strategy != kubevirtv1.RunStrategyAlways {
		t.Errorf("expected run strategy '%s', got: %v", kubevirtv1.RunStrategyAlways, strategy)
	}
}

func TestLinuxProbeFollowsCloudInitStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the probe is a shell command")
	}
	probe := buildProbeExecCommand(vm.Linux)

	for name, test := range map[string]struct {
		status   string
		exitCode int
		ready    bool
	}{
		"done":               {status: "done", exitCode: 0, ready: true},
		"done with warnings": {status: "done", exitCode: 2, ready: true},
		"running":            {status: "running", exitCode: 0, ready: false},
		"error":              {status: "error", exitCode: 1, ready: false},
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			script := fmt.Sprintf("#!/bin/sh\necho 'status: %s'\nexit %d\n", test.status, test.exitCode)
			if err := os.WriteFile(filepath.Join(directory, "cloud-init"), []byte(script), 0o755); err != nil {
				t.Fatalf("failed to write the cloud-init stand-in: %v", err)
			}

			t.Setenv("PATH", directory+":/usr/bin:/bin")
			err := exec.Command(probe[0], probe[1:]...).Run()
			if ready := err == nil; ready != test.ready {
				t.Errorf("expected ready to be %t, got: %t (%v)", test.ready, ready, err)
			}
		})
	}
}

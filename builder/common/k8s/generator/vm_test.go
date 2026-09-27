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

func generateWindowsVirtualMachine() *kubevirtv1.VirtualMachine {
	return GenerateVirtualMachine(VirtualMachineOptions{
		Name:        "base-windows",
		Namespace:   "packer",
		OsFamily:    vm.Windows,
		DiskSpace:   "64Gi",
		CPU:         "2",
		Memory:      "4Gi",
		ImageSource: ImageSource{URL: "https://example.com/windows.iso"},
	})
}

func findDataVolumeTemplate(virtualMachine *kubevirtv1.VirtualMachine, name string) *kubevirtv1.DataVolumeTemplateSpec {
	for index, template := range virtualMachine.Spec.DataVolumeTemplates {
		if template.Name == name {
			return &virtualMachine.Spec.DataVolumeTemplates[index]
		}
	}
	return nil
}

func TestWindowsSystemDiskIsExported(t *testing.T) {
	virtualMachine := generateWindowsVirtualMachine()

	// the export step and the post-processors look for the volume named after the source suffix
	systemDisk := BuildDataVolumeName("base-windows", SourceDataVolumeSuffix)
	volume := virtualMachine.Spec.Template.Spec.Volumes[0]
	if volume.Name != string(PrimaryVolumeDiskMapping) {
		t.Fatalf("expected the first volume to be '%s', got: '%s'", PrimaryVolumeDiskMapping, volume.Name)
	}
	if volume.EmptyDisk != nil {
		t.Errorf("expected the system disk to outlive the Virtual Machine, got an empty disk")
	}
	if volume.DataVolume == nil || volume.DataVolume.Name != systemDisk {
		t.Fatalf("expected the system disk to be the Data Volume '%s', got: %+v", systemDisk, volume.VolumeSource)
	}

	template := findDataVolumeTemplate(virtualMachine, systemDisk)
	if template == nil {
		t.Fatalf("expected a Data Volume template named '%s'", systemDisk)
	}
	if template.Spec.Source == nil || template.Spec.Source.Blank == nil {
		t.Errorf("expected the system disk to start blank, got: %+v", template.Spec.Source)
	}
	if size := template.Spec.PVC.Resources.Requests[corev1.ResourceStorage]; size.String() != "64Gi" {
		t.Errorf("expected a system disk of 64Gi, got: %s", size.String())
	}
}

func TestWindowsInstallMediaIsImportedFromTheSource(t *testing.T) {
	virtualMachine := generateWindowsVirtualMachine()

	installMedia := BuildDataVolumeName("base-windows", InstallMediaDataVolumeSuffix)
	template := findDataVolumeTemplate(virtualMachine, installMedia)
	if template == nil {
		t.Fatalf("expected a Data Volume template named '%s'", installMedia)
	}
	if template.Spec.Source == nil || template.Spec.Source.HTTP == nil || template.Spec.Source.HTTP.URL != "https://example.com/windows.iso" {
		t.Errorf("expected the install media to be imported from the source URL, got: %+v", template.Spec.Source)
	}

	for _, volume := range virtualMachine.Spec.Template.Spec.Volumes {
		if volume.Name == string(IsoInstallVolumeDiskMapping) {
			if volume.DataVolume == nil || volume.DataVolume.Name != installMedia {
				t.Errorf("expected the install volume to be the Data Volume '%s', got: %+v", installMedia, volume.VolumeSource)
			}
			return
		}
	}
	t.Errorf("expected a volume named '%s'", IsoInstallVolumeDiskMapping)
}

func TestWindowsBootsFromTheSystemDiskFirst(t *testing.T) {
	virtualMachine := generateWindowsVirtualMachine()

	bootOrders := map[string]uint{}
	for _, disk := range virtualMachine.Spec.Template.Spec.Domain.Devices.Disks {
		if disk.BootOrder != nil {
			bootOrders[disk.Name] = *disk.BootOrder
		}
	}
	// setup reboots into the installed system, the firmware only tries disks that have a boot order
	if order := bootOrders[string(PrimaryVolumeDiskMapping)]; order != 1 {
		t.Errorf("expected the system disk to boot first, got boot order: %d", order)
	}
	if order := bootOrders[string(IsoInstallVolumeDiskMapping)]; order != 2 {
		t.Errorf("expected the install media to boot second, got boot order: %d", order)
	}
}

func TestWindowsSystemDiskBusIsLeftToThePreference(t *testing.T) {
	virtualMachine := generateWindowsVirtualMachine()

	for _, disk := range virtualMachine.Spec.Template.Spec.Domain.Devices.Disks {
		if disk.Name != string(PrimaryVolumeDiskMapping) {
			continue
		}
		// a bus set here wins over the one of the preference, 'windows.11.virtio' would install on SATA
		if disk.Disk == nil || disk.Disk.Bus != "" {
			t.Errorf("expected the system disk to have no bus, got: %+v", disk.DiskDevice)
		}
		return
	}
	t.Errorf("expected a disk named '%s'", PrimaryVolumeDiskMapping)
}

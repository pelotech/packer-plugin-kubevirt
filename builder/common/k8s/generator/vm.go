package generator

import (
	_ "embed"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	cdiv1beta1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
	"packer-plugin-kubevirt/builder/common/vm"
)

//go:embed scripts/cloud-init.yaml
var defaultCloudInit string

//go:embed scripts/autounattend.xml
var defaultAutounattend string

const (
	defaultNetworkName = "default"
	virtioDriversURL   = "https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/virtio-win.iso"
	defaultMacAddress  = "00:00:00:00:00:00"
)

type VirtualMachineOptions struct {
	Name             string
	Namespace        string
	NodeSelector     map[string]string
	Tolerations      []corev1.Toleration
	Preference       string
	OsFamily         vm.OsFamily
	DiskSize         string
	CPU              string
	Memory           string
	ImageSource      ImageSource
	UserProvisioning UserProvisioning
}

type ImageSource struct {
	URL                string
	AWSAccessKeyId     string
	AWSSecretAccessKey string
}

func (s ImageSource) HasS3Credentials() bool {
	return s.AWSAccessKeyId != "" && s.AWSSecretAccessKey != ""
}

type UserProvisioning struct {
	CloudInit    string
	Autounattend string
}

type SecretSuffix string

const (
	StartupScriptSecretSuffix SecretSuffix = "startup-scripts"
	S3CredentialsSuffix       SecretSuffix = "s3-credentials"
)

func buildSecretName(vmName string, suffix SecretSuffix) string {
	return fmt.Sprintf("%s-%s", vmName, suffix)
}

type VolumeDiskMapping string

const (
	PrimaryVolumeDiskMapping       VolumeDiskMapping = "primary"
	CloudInitVolumeDiskMapping     VolumeDiskMapping = "cloud-init"
	SysprepInitVolumeDiskMapping   VolumeDiskMapping = "sysprep-init"
	IsoInstallVolumeDiskMapping    VolumeDiskMapping = "iso-install"
	VirtioDriversVolumeDiskMapping VolumeDiskMapping = "virtio-drivers"
)

type DataVolumeSuffix string

const (
	// SourceDataVolumeSuffix names the disk of the Virtual Machine, the one that is exported
	SourceDataVolumeSuffix       DataVolumeSuffix = "source"
	InstallMediaDataVolumeSuffix DataVolumeSuffix = "install-media"
	VirtioDataVolumeSuffix       DataVolumeSuffix = "virtio-drivers"
)

func BuildDataVolumeName(vmName string, suffix DataVolumeSuffix) string {
	return fmt.Sprintf("%s-%s", vmName, suffix)
}

func buildProbeExecCommand(family vm.OsFamily) []string {
	var command []string
	switch family {
	case vm.Linux:
		command = []string{
			// cloud-init exits with 2 when it is done with warnings
			"/bin/sh",
			"-c",
			"cloud-init status | grep -q 'status: done'",
		}
	case vm.Windows:
		command = []string{
			// NOTE: echo is 'acceptable' because qemu-ga is the last tool provisioned through autounattend.xml.
			"cmd",
			"/c",
			"echo",
		}
	}

	return command
}

func GenerateStartupScriptSecret(virtualMachine *kubevirtv1.VirtualMachine, opts VirtualMachineOptions) *corev1.Secret {
	key, script, defaultScript := "userData", opts.UserProvisioning.CloudInit, defaultCloudInit
	if opts.OsFamily == vm.Windows {
		key, script, defaultScript = "autounattend.xml", opts.UserProvisioning.Autounattend, defaultAutounattend
	}
	if script == "" {
		script = defaultScript
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildSecretName(opts.Name, StartupScriptSecretSuffix),
			Namespace: opts.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(virtualMachine, kubevirtv1.VirtualMachineGroupVersionKind),
			},
		},
		StringData: map[string]string{key: script},
		Type:       corev1.SecretTypeOpaque,
	}
}

func GenerateS3CredentialsSecret(vm *kubevirtv1.VirtualMachine, opts VirtualMachineOptions) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildSecretName(opts.Name, S3CredentialsSuffix),
			Namespace: opts.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(vm, kubevirtv1.VirtualMachineGroupVersionKind),
			},
		},
		StringData: map[string]string{
			"accessKeyId": opts.ImageSource.AWSAccessKeyId,
			"secretKey":   opts.ImageSource.AWSSecretAccessKey,
		},
		Type: corev1.SecretTypeOpaque,
	}
}

func GenerateVirtualMachine(opts VirtualMachineOptions) *kubevirtv1.VirtualMachine {
	runStrategy := kubevirtv1.RunStrategyOnce
	disks := generateDisks(opts.OsFamily)
	volumes := generateVolumes(opts)
	probeExecCommand := buildProbeExecCommand(opts.OsFamily)

	var dataVolumeSource cdiv1beta1.DataVolumeSource
	if opts.ImageSource.HasS3Credentials() {
		secretName := buildSecretName(opts.Name, S3CredentialsSuffix)
		dataVolumeSource = cdiv1beta1.DataVolumeSource{
			S3: &cdiv1beta1.DataVolumeSourceS3{
				URL:       opts.ImageSource.URL,
				SecretRef: secretName,
			},
		}
	} else {
		dataVolumeSource = cdiv1beta1.DataVolumeSource{
			HTTP: &cdiv1beta1.DataVolumeSourceHTTP{
				URL: opts.ImageSource.URL,
			},
		}
	}
	dataVolumeTemplates := generateDataVolumeTemplates(opts.OsFamily, dataVolumeSource, opts.Name, opts.DiskSize)

	return &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.Name,
			Namespace: opts.Namespace,
		},
		Spec: kubevirtv1.VirtualMachineSpec{
			RunStrategy: &runStrategy,
			Preference: &kubevirtv1.PreferenceMatcher{
				Kind: "VirtualMachineClusterPreference",
				Name: opts.Preference,
			},
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					NodeSelector: opts.NodeSelector,
					Tolerations:  opts.Tolerations,
					ReadinessProbe: &kubevirtv1.Probe{
						Handler: kubevirtv1.Handler{
							Exec: &corev1.ExecAction{
								Command: probeExecCommand,
							},
						},
						InitialDelaySeconds: 30,
						PeriodSeconds:       10,
					},
					Domain: kubevirtv1.DomainSpec{
						Resources: kubevirtv1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(opts.CPU),
								corev1.ResourceMemory: resource.MustParse(opts.Memory),
							},
						},
						Devices: kubevirtv1.Devices{
							Disks: disks,
							Interfaces: []kubevirtv1.Interface{
								{
									Name:       defaultNetworkName,
									MacAddress: defaultMacAddress,
									InterfaceBindingMethod: kubevirtv1.InterfaceBindingMethod{
										Masquerade: &kubevirtv1.InterfaceMasquerade{},
									},
								},
							},
						},
					},
					Volumes: volumes,
					Networks: []kubevirtv1.Network{
						{
							Name: defaultNetworkName,
							NetworkSource: kubevirtv1.NetworkSource{
								Pod: &kubevirtv1.PodNetwork{},
							},
						},
					},
				},
			},
			DataVolumeTemplates: dataVolumeTemplates,
		},
	}
}

func generateDataVolumeTemplates(family vm.OsFamily, dvSource cdiv1beta1.DataVolumeSource, vmName, vmPrimaryDiskSize string) []kubevirtv1.DataVolumeTemplateSpec {
	if family != vm.Windows {
		return []kubevirtv1.DataVolumeTemplateSpec{
			dataVolumeTemplate(BuildDataVolumeName(vmName, SourceDataVolumeSuffix), vmPrimaryDiskSize, dvSource),
		}
	}

	return []kubevirtv1.DataVolumeTemplateSpec{
		// Disk empty and used as target by Windows install
		dataVolumeTemplate(BuildDataVolumeName(vmName, SourceDataVolumeSuffix), vmPrimaryDiskSize, cdiv1beta1.DataVolumeSource{Blank: &cdiv1beta1.DataVolumeBlankImage{}}),
		dataVolumeTemplate(BuildDataVolumeName(vmName, InstallMediaDataVolumeSuffix), vmPrimaryDiskSize, dvSource),
		dataVolumeTemplate(BuildDataVolumeName(vmName, VirtioDataVolumeSuffix), "1Gi", cdiv1beta1.DataVolumeSource{HTTP: &cdiv1beta1.DataVolumeSourceHTTP{URL: virtioDriversURL}}),
	}
}

func dataVolumeTemplate(name, size string, source cdiv1beta1.DataVolumeSource) kubevirtv1.DataVolumeTemplateSpec {
	return kubevirtv1.DataVolumeTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: cdiv1beta1.DataVolumeSpec{
			PVC: &corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(size),
					},
				},
			},
			Source: &source,
		},
	}
}

/*
*
-- FUTURE STATE
Decision drivers:
- Spec definition without branching between OS families, not necessarily being the main driver for disk/volume definition
- Iteration speed, if the Windows configuration fails, the iteration should resume from post-install
- Parallelism, once system is installed, running all the different configurations in parallel
Considered Options:
1. Extra optional step in the same builder building a VM in charge of the Windows install only
2. New builder dedicated to the installation, second builder type parallelizing Linux/Windows base images
Tradeoffs:
 1. +: Integrated
    -: all the Windows configurations running the same install part of its lifecycle
 2. +: Parallelism
    -: Orchestration managed outside of Packer anyway needed for step 3 (1. run installs 2. run base images 3. run lab images)
*/
func generateDisks(family vm.OsFamily) []kubevirtv1.Disk {
	var disks []kubevirtv1.Disk

	switch family {
	case vm.Linux:
		disks = append(disks,
			kubevirtv1.Disk{
				Name: string(PrimaryVolumeDiskMapping),
				DiskDevice: kubevirtv1.DiskDevice{
					Disk: &kubevirtv1.DiskTarget{
						Bus: kubevirtv1.DiskBusVirtio,
					},
				},
			},
			kubevirtv1.Disk{
				Name: string(CloudInitVolumeDiskMapping),
				DiskDevice: kubevirtv1.DiskDevice{
					Disk: &kubevirtv1.DiskTarget{
						Bus: kubevirtv1.DiskBusVirtio,
					},
				},
			})
	case vm.Windows:
		// the blank disk cannot boot, so the install media starts until Windows is installed
		primaryBootOrder, installBootOrder := uint(1), uint(2)
		disks = append(disks,
			// Disk C: its bus is the one of the preference
			kubevirtv1.Disk{
				Name:      string(PrimaryVolumeDiskMapping),
				BootOrder: &primaryBootOrder,
				DiskDevice: kubevirtv1.DiskDevice{
					Disk: &kubevirtv1.DiskTarget{},
				},
			},
			// Disk D:
			kubevirtv1.Disk{
				Name:      string(IsoInstallVolumeDiskMapping),
				BootOrder: &installBootOrder,
				DiskDevice: kubevirtv1.DiskDevice{
					CDRom: &kubevirtv1.CDRomTarget{
						Bus: kubevirtv1.DiskBusSATA,
					},
				},
			},
			// Disk E: (virtio drivers) - HAS TO match `autounattend.xml` disk letter for virtio
			kubevirtv1.Disk{
				Name: string(VirtioDriversVolumeDiskMapping),
				DiskDevice: kubevirtv1.DiskDevice{
					CDRom: &kubevirtv1.CDRomTarget{
						Bus: kubevirtv1.DiskBusSATA,
					},
				},
			},
			// Disk F: (autounattend.xml)
			kubevirtv1.Disk{
				Name: string(SysprepInitVolumeDiskMapping),
				DiskDevice: kubevirtv1.DiskDevice{
					CDRom: &kubevirtv1.CDRomTarget{
						Bus: kubevirtv1.DiskBusSATA,
					},
				},
			})
	}

	return disks
}

func generateVolumes(opts VirtualMachineOptions) []kubevirtv1.Volume {
	volumes := []kubevirtv1.Volume{
		{
			Name: string(PrimaryVolumeDiskMapping),
			VolumeSource: kubevirtv1.VolumeSource{
				DataVolume: &kubevirtv1.DataVolumeSource{
					Name: BuildDataVolumeName(opts.Name, SourceDataVolumeSuffix),
				},
			},
		},
	}

	switch opts.OsFamily {
	case vm.Linux:
		volumes = append(volumes,
			kubevirtv1.Volume{
				Name: string(CloudInitVolumeDiskMapping),
				VolumeSource: kubevirtv1.VolumeSource{
					CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
						UserDataSecretRef: &corev1.LocalObjectReference{
							Name: buildSecretName(opts.Name, StartupScriptSecretSuffix),
						},
					},
				},
			},
		)
	case vm.Windows:
		volumes = append(volumes,
			kubevirtv1.Volume{
				Name: string(IsoInstallVolumeDiskMapping),
				VolumeSource: kubevirtv1.VolumeSource{
					DataVolume: &kubevirtv1.DataVolumeSource{
						Name: BuildDataVolumeName(opts.Name, InstallMediaDataVolumeSuffix),
					},
				},
			},
			kubevirtv1.Volume{
				Name: string(VirtioDriversVolumeDiskMapping),
				VolumeSource: kubevirtv1.VolumeSource{
					DataVolume: &kubevirtv1.DataVolumeSource{
						Name: BuildDataVolumeName(opts.Name, VirtioDataVolumeSuffix),
					},
				},
			},
			kubevirtv1.Volume{
				Name: string(SysprepInitVolumeDiskMapping),
				VolumeSource: kubevirtv1.VolumeSource{
					Sysprep: &kubevirtv1.SysprepSource{
						Secret: &corev1.LocalObjectReference{
							Name: buildSecretName(opts.Name, StartupScriptSecretSuffix),
						},
					},
				},
			},
		)
	}

	return volumes
}

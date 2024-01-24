package vm_generator

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	cdiv1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/utils"
	"strings"
)

type VirtualMachineOptions struct {
	Name                    string
	Namespace               string
	OsPreference            string
	S3ImageSource           S3ImageSource
	Credentials             *AccessCredentials
	DiskSpace               string
	StartupScriptSecretName string
}

type AccessCredentials struct {
	Username string
	Password string
}

type S3ImageSource struct {
	Url        string
	SecretName string
}

type OsFamily int32

const (
	Linux   OsFamily = 0
	Windows          = 1
)

func GetOSFamily(preference string) OsFamily {
	if strings.Contains(strings.ToLower(preference), "windows") {
		return Windows
	}
	return Linux
}

func generateProbeExecCommand(family OsFamily) []string {
	var command []string
	switch family {
	case Linux:
		command = []string{
			"/bin/sh",
			"-c",
			"cloud-init",
			"status",
		}
	case Windows:
		command = []string{
			"cmd",
			"/c",
			"findstr",
			"IMAGE_STATE_COMPLETE",
			"%SystemRoot%\\Setup\\State\\state.ini",
		}
	}

	return command
}

func GenerateStartupScriptSecret(vm *kubevirtv1.VirtualMachine, opts VirtualMachineOptions) *corev1.Secret {
	data := make(map[string]string)
	switch GetOSFamily(opts.OsPreference) {
	case Linux:
		filename := "cloud-init.yaml"
		data["userdata"] = utils.ReadFile("scripts", filename)
	case Windows:
		filename := "autounattend.xml"
		data[filename] = utils.ReadFile("scripts", filename)
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.StartupScriptSecretName,
			Namespace: opts.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(vm, k8s.VirtualMachineGroupVersionKind),
			},
		},
		StringData: data,
		Type:       corev1.SecretTypeOpaque,
	}
}

func GenerateCredentials(vm *kubevirtv1.VirtualMachine, opts VirtualMachineOptions) *corev1.Secret {
	password := opts.Credentials.Password
	if opts.Credentials.Password == "" {
		password = utils.GenerateRandomPassword(20)
	}
	data := map[string]string{
		opts.Credentials.Username: password,
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.Join([]string{opts.Name, "credentials"}, "-"),
			Namespace: opts.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(vm, k8s.VirtualMachineGroupVersionKind),
			},
		},
		StringData: data,
		Type:       corev1.SecretTypeOpaque,
	}
}

func GenerateVirtualMachine(opts VirtualMachineOptions) *kubevirtv1.VirtualMachine {
	osFamily := GetOSFamily(opts.OsPreference)
	disks := generateDisks(osFamily, opts)
	volumes := generateVolumes(osFamily, opts)
	probeExecCommand := generateProbeExecCommand(osFamily)

	var accessCredentials []kubevirtv1.AccessCredential
	if opts.Credentials != nil {
		secretName := strings.Join([]string{opts.Name, "credentials"}, "-")
		accessCredentials = append(accessCredentials, generateUserPasswordAccessCredential(secretName))
	}

	return &kubevirtv1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.Name,
			Namespace: opts.Namespace,
		},
		Spec: kubevirtv1.VirtualMachineSpec{
			Preference: &kubevirtv1.PreferenceMatcher{
				Kind: "VirtualMachineClusterPreference",
				Name: opts.OsPreference,
			},
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					ReadinessProbe: &kubevirtv1.Probe{
						Handler: kubevirtv1.Handler{
							Exec: &corev1.ExecAction{
								Command: probeExecCommand,
							},
						},
					},
					AccessCredentials: accessCredentials,
					Domain: kubevirtv1.DomainSpec{
						Resources: kubevirtv1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("4"),
								corev1.ResourceMemory: resource.MustParse("8Gi"),
							},
						},
						Devices: kubevirtv1.Devices{
							Disks: disks,
						},
					},
					Volumes: volumes,
				},
			},
			DataVolumeTemplates: []kubevirtv1.DataVolumeTemplateSpec{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("%s-source", opts.Name),
					},
					Spec: cdiv1.DataVolumeSpec{
						PVC: &corev1.PersistentVolumeClaimSpec{
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse(opts.DiskSpace),
								},
							},
						},
						Source: &cdiv1.DataVolumeSource{
							S3: &cdiv1.DataVolumeSourceS3{
								URL:       opts.S3ImageSource.Url,
								SecretRef: opts.S3ImageSource.SecretName,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("%s-virtio-drivers", opts.Name),
					},
					Spec: cdiv1.DataVolumeSpec{
						PVC: &corev1.PersistentVolumeClaimSpec{
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("1Gi"),
								},
							},
						},
						Source: &cdiv1.DataVolumeSource{
							HTTP: &cdiv1.DataVolumeSourceHTTP{
								URL: "https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/virtio-win.iso",
							},
						},
					},
				},
			},
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
func generateDisks(family OsFamily, opts VirtualMachineOptions) []kubevirtv1.Disk {
	disks := []kubevirtv1.Disk{{
		Name: "primary",
		DiskDevice: kubevirtv1.DiskDevice{
			Disk: &kubevirtv1.DiskTarget{
				Bus: kubevirtv1.DiskBusVirtio,
			},
		},
	}}

	switch family {
	case Linux:
		if opts.StartupScriptSecretName == "" {
			return disks
		}
		disks = append(disks,
			kubevirtv1.Disk{
				Name: "cloud-init",
				DiskDevice: kubevirtv1.DiskDevice{
					Disk: &kubevirtv1.DiskTarget{
						Bus: kubevirtv1.DiskBusVirtio,
					},
				},
			})
	case Windows:
		bootOrder := uint(1)
		disks = append(disks,
			// Disk D:
			kubevirtv1.Disk{
				Name:      "iso-install",
				BootOrder: &bootOrder,
				DiskDevice: kubevirtv1.DiskDevice{
					CDRom: &kubevirtv1.CDRomTarget{
						Bus: kubevirtv1.DiskBusSATA,
					},
				},
			},
			// Disk E: (virtio drivers) - HAS TO match `autounattend.xml` disk letter for virtio
			kubevirtv1.Disk{
				Name: "virtio-drivers",
				DiskDevice: kubevirtv1.DiskDevice{
					CDRom: &kubevirtv1.CDRomTarget{
						Bus: kubevirtv1.DiskBusSATA,
					},
				},
			},
		)

		if opts.StartupScriptSecretName != "" {
			disks = append(disks,
				// Disk F: (sysprep)
				kubevirtv1.Disk{
					Name: "sysprep-init",
					DiskDevice: kubevirtv1.DiskDevice{
						CDRom: &kubevirtv1.CDRomTarget{
							Bus: kubevirtv1.DiskBusSATA,
						},
					},
				})
		}
	}

	return disks
}

func generateVolumes(family OsFamily, opts VirtualMachineOptions) []kubevirtv1.Volume {
	primaryVolumeSource := kubevirtv1.VolumeSource{}
	switch family {
	case Linux:
		primaryVolumeSource.DataVolume = &kubevirtv1.DataVolumeSource{
			// Disk mounted with Linux cloud image
			Name: fmt.Sprintf("%s-source", opts.Name),
		}
	case Windows:
		primaryVolumeSource.EmptyDisk = &kubevirtv1.EmptyDiskSource{
			// Disk empty and used as target by Windows install
			Capacity: resource.MustParse(opts.DiskSpace),
		}
	}

	volumes := []kubevirtv1.Volume{
		{
			Name:         "primary",
			VolumeSource: primaryVolumeSource,
		},
	}

	switch family {
	case Linux:
		if opts.StartupScriptSecretName == "" {
			return volumes
		}
		volumes = append(volumes,
			kubevirtv1.Volume{
				Name: "cloud-init",
				VolumeSource: kubevirtv1.VolumeSource{
					CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
						UserDataSecretRef: &corev1.LocalObjectReference{
							Name: opts.StartupScriptSecretName,
						},
					},
				},
			},
		)
	case Windows:
		volumes = append(volumes,
			kubevirtv1.Volume{
				Name: "iso-install",
				VolumeSource: kubevirtv1.VolumeSource{
					DataVolume: &kubevirtv1.DataVolumeSource{
						Name: fmt.Sprintf("%s-source", opts.Name),
					},
				},
			},
			kubevirtv1.Volume{
				Name: "virtio-drivers",
				VolumeSource: kubevirtv1.VolumeSource{
					DataVolume: &kubevirtv1.DataVolumeSource{
						Name: fmt.Sprintf("%s-virtio-drivers", opts.Name),
					},
				},
			})

		if opts.StartupScriptSecretName != "" {
			volumes = append(volumes,
				kubevirtv1.Volume{
					Name: "sysprep-init",
					VolumeSource: kubevirtv1.VolumeSource{
						Sysprep: &kubevirtv1.SysprepSource{
							Secret: &corev1.LocalObjectReference{
								Name: opts.StartupScriptSecretName,
							},
						},
					},
				})
		}
	}

	return volumes
}

func generateUserPasswordAccessCredential(secretName string) kubevirtv1.AccessCredential {
	return kubevirtv1.AccessCredential{
		UserPassword: &kubevirtv1.UserPasswordAccessCredential{
			Source: kubevirtv1.UserPasswordAccessCredentialSource{
				Secret: &kubevirtv1.AccessCredentialSecretSource{
					SecretName: secretName,
				},
			},
			PropagationMethod: kubevirtv1.UserPasswordAccessCredentialPropagationMethod{
				QemuGuestAgent: &kubevirtv1.QemuGuestAgentUserPasswordAccessCredentialPropagation{},
			},
		},
	}
}

package resourcegenerator

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	cdiv1beta1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
	"packer-plugin-kubevirt/builder/common/utils"
	"strings"
)

type VirtualMachineOptions struct {
	Name          string
	Namespace     string
	OsPreference  string
	S3ImageSource S3ImageSource
	Credentials   *AccessCredentials
	DiskSpace     string
}

type AccessCredentials struct {
	Username string
	Password string
}

type S3ImageSource struct {
	URL                string
	AWSAccessKeyId     string
	AWSSecretAccessKey string
}

type OsFamily int32

const (
	Linux   OsFamily = 0
	Windows          = 1
)

type SecretSuffix string

const (
	StartupScriptSecretSuffix SecretSuffix = "startup-scripts"
	UserCredentialsSuffix     SecretSuffix = "user-credentials"
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
	SourceDataVolumeSuffix DataVolumeSuffix = "source"
	VirtioDataVolumeSuffix DataVolumeSuffix = "virtio-drivers"
)

func buildDataVolumeName(vmName string, suffix DataVolumeSuffix) string {
	return fmt.Sprintf("%s-%s", vmName, suffix)
}

func getOSFamily(preference string) OsFamily {
	if strings.Contains(strings.ToLower(preference), "windows") {
		return Windows
	}
	return Linux
}

func buildProbeExecCommand(family OsFamily) []string {
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
	scriptsDir := "scripts"
	switch getOSFamily(opts.OsPreference) {
	case Linux:
		filename := "cloud-init.yaml"
		data["userdata"] = utils.ReadFile(scriptsDir, filename)
	case Windows:
		filename := "autounattend.xml"
		data[filename] = utils.ReadFile(scriptsDir, filename)
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildSecretName(opts.Name, StartupScriptSecretSuffix),
			Namespace: opts.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(vm, kubevirtv1.VirtualMachineGroupVersionKind),
			},
		},
		StringData: data,
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
			"accessKeyId": opts.S3ImageSource.AWSAccessKeyId,
			"secretKey":   opts.S3ImageSource.AWSSecretAccessKey,
		},
		Type: corev1.SecretTypeOpaque,
	}
}

func GenerateUserCredentialsSecret(vm *kubevirtv1.VirtualMachine, opts VirtualMachineOptions) *corev1.Secret {
	password := opts.Credentials.Password
	if opts.Credentials.Password == "" {
		password = utils.GenerateRandomPassword(20)
	}
	data := map[string]string{
		opts.Credentials.Username: password,
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildSecretName(opts.Name, UserCredentialsSuffix),
			Namespace: opts.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(vm, kubevirtv1.VirtualMachineGroupVersionKind),
			},
		},
		StringData: data,
		Type:       corev1.SecretTypeOpaque,
	}
}

func GenerateVirtualMachine(opts VirtualMachineOptions) *kubevirtv1.VirtualMachine {
	osFamily := getOSFamily(opts.OsPreference)
	disks := generateDisks(osFamily, opts)
	volumes := generateVolumes(osFamily, opts)
	probeExecCommand := buildProbeExecCommand(osFamily)

	var accessCredentials []kubevirtv1.AccessCredential
	if opts.Credentials != nil {
		secretName := buildSecretName(opts.Name, UserCredentialsSuffix)
		accessCredentials = append(accessCredentials, generateUserPasswordAccessCredential(secretName))
	}

	var secretName string
	if opts.S3ImageSource.AWSAccessKeyId != "" && opts.S3ImageSource.AWSSecretAccessKey != "" {
		secretName = buildSecretName(opts.Name, S3CredentialsSuffix)
	}
	dataVolumeSourceS3 := &cdiv1beta1.DataVolumeSourceS3{
		URL:       opts.S3ImageSource.URL,
		SecretRef: secretName,
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
						InitialDelaySeconds: 30,
						PeriodSeconds:       10,
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
						Name: buildDataVolumeName(opts.Name, SourceDataVolumeSuffix),
					},
					Spec: cdiv1beta1.DataVolumeSpec{
						PVC: &corev1.PersistentVolumeClaimSpec{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse(opts.DiskSpace),
								},
							},
						},
						Source: &cdiv1beta1.DataVolumeSource{
							S3: dataVolumeSourceS3,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: buildDataVolumeName(opts.Name, VirtioDataVolumeSuffix),
					},
					Spec: cdiv1beta1.DataVolumeSpec{
						PVC: &corev1.PersistentVolumeClaimSpec{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("1Gi"),
								},
							},
						},
						Source: &cdiv1beta1.DataVolumeSource{
							HTTP: &cdiv1beta1.DataVolumeSourceHTTP{
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
		Name: string(PrimaryVolumeDiskMapping),
		DiskDevice: kubevirtv1.DiskDevice{
			Disk: &kubevirtv1.DiskTarget{
				Bus: kubevirtv1.DiskBusVirtio,
			},
		},
	}}

	switch family {
	case Linux:
		disks = append(disks,
			kubevirtv1.Disk{
				Name: string(CloudInitVolumeDiskMapping),
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
				Name:      string(IsoInstallVolumeDiskMapping),
				BootOrder: &bootOrder,
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
			// Disk F: (sysprep)
			kubevirtv1.Disk{
				Name: string(SysprepInitVolumeDiskMapping),
				DiskDevice: kubevirtv1.DiskDevice{
					CDRom: &kubevirtv1.CDRomTarget{
						Bus: kubevirtv1.DiskBusSATA,
					},
				},
			},
		)
	}

	return disks
}

func generateVolumes(family OsFamily, opts VirtualMachineOptions) []kubevirtv1.Volume {
	primaryVolumeSource := kubevirtv1.VolumeSource{}
	switch family {
	case Linux:
		// Disk mounted with Linux cloud image
		primaryVolumeSource.DataVolume = &kubevirtv1.DataVolumeSource{
			Name: buildDataVolumeName(opts.Name, SourceDataVolumeSuffix),
		}
	case Windows:
		// Disk empty and used as target by Windows install
		primaryVolumeSource.EmptyDisk = &kubevirtv1.EmptyDiskSource{
			Capacity: resource.MustParse(opts.DiskSpace),
		}
	}

	volumes := []kubevirtv1.Volume{
		{
			Name:         string(PrimaryVolumeDiskMapping),
			VolumeSource: primaryVolumeSource,
		},
	}

	switch family {
	case Linux:
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
	case Windows:
		volumes = append(volumes,
			kubevirtv1.Volume{
				Name: string(IsoInstallVolumeDiskMapping),
				VolumeSource: kubevirtv1.VolumeSource{
					DataVolume: &kubevirtv1.DataVolumeSource{
						Name: buildDataVolumeName(opts.Name, SourceDataVolumeSuffix),
					},
				},
			},
			kubevirtv1.Volume{
				Name: string(VirtioDriversVolumeDiskMapping),
				VolumeSource: kubevirtv1.VolumeSource{
					DataVolume: &kubevirtv1.DataVolumeSource{
						Name: buildDataVolumeName(opts.Name, VirtioDataVolumeSuffix),
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

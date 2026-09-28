package generator

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"path"
)

const (
	// GuestFSJobNameSuffix follows the name of the Virtual Machine in the name of the generalize job
	GuestFSJobNameSuffix = "-libguestfs"
	homeDirVolumeName    = "guestfs"
	homeDirPath          = "/home/guestfs"
	vmDiskVolumeName     = "volume"
	vmDiskPath           = "/disk"
	tmpDirVolumeName     = "libguestfs-tmp-dir"
	tmpDirPath           = "/tmp/guestfs"
)

func GenerateGuestFSJob(vm *kubevirtv1.VirtualMachine, userToKeep string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vm.Name + GuestFSJobNameSuffix,
			Namespace: vm.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(vm, kubevirtv1.VirtualMachineGroupVersionKind),
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: ptr.To[int32](30),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector:  vm.Spec.Template.Spec.NodeSelector,
					Tolerations:   vm.Spec.Template.Spec.Tolerations,
					RestartPolicy: corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(false),
						RunAsUser:    ptr.To[int64](0),
						RunAsGroup:   ptr.To[int64](0),
						FSGroup:      ptr.To[int64](0),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "libguestfs",
							Image: "quay.io/kubevirt/libguestfs-tools:v1.2.0",
							Command: []string{
								"virt-sysprep",
								"--verbose",
								"--add",
								path.Join(vmDiskPath, "disk.img"),
								"--network",
								"--enable",
								"bash-history,machine-id,user-account",
								"--keep-user-accounts",
								userToKeep,
							},
							WorkingDir: vmDiskPath,
							// LIBGUESTFS_BACKEND  -> use directly host qemu
							// LIBGUESTFS_PATH 	   -> path to root, initrd and the kernel are located
							// LIBGUESTFS_TMPDIR   -> path to libguestfs temporary files are generated
							// HOME 			   -> path to user libvirt cache
							Env: []corev1.EnvVar{
								{
									Name:  "LIBGUESTFS_BACKEND",
									Value: "direct",
								},
								{
									Name:  "LIBGUESTFS_PATH",
									Value: "/usr/local/lib/guestfs/appliance",
								},
								{
									Name:  "LIBGUESTFS_TMPDIR",
									Value: tmpDirPath,
								},
								{
									Name:  "HOME",
									Value: homeDirPath,
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							Stdin: true,
							TTY:   true,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      vmDiskVolumeName,
									ReadOnly:  false,
									MountPath: vmDiskPath,
								},
								{
									Name:      tmpDirVolumeName,
									ReadOnly:  false,
									MountPath: tmpDirPath,
								},
								{
									Name:      homeDirVolumeName,
									ReadOnly:  false,
									MountPath: homeDirPath,
								},
							},
							ImagePullPolicy: corev1.PullIfNotPresent,
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"devices.kubevirt.io/kvm": resource.MustParse("1"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: vmDiskVolumeName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: BuildDataVolumeName(vm.Name, SourceDataVolumeSuffix),
									ReadOnly:  false,
								},
							},
						},
						{
							Name: tmpDirVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: homeDirVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}
}

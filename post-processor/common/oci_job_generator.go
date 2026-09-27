package common

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	exportv1 "kubevirt.io/api/export/v1"
	"packer-plugin-kubevirt/builder/common/k8s"
	"path"
	"strings"
)

const (
	dockerConfigVolumeMountVolumeMapping = "docker-config"
	dockerConfigVolumeMountPath          = "/docker-config"
	dockerConfigFilename                 = "config.json"
	dockerHubRegistry                    = "https://index.docker.io/v1/"
	ociJobSecretSuffix                   = "oci-uploader"
	pushImage                            = "gcr.io/go-containerregistry/krane:v0.22.1"
	imageArchiveFilename                 = "image.tar"
	imageConfigEnvVar                    = "IMAGE_CONFIG"
	architecturePlaceholder              = "ARCHITECTURE"
	layerDigestPlaceholder               = "LAYER_DIGEST"
	defaultPreferenceEnvVar              = "INSTANCETYPE_KUBEVIRT_IO_DEFAULT_PREFERENCE"
	defaultInstanceTypeEnvVar            = "INSTANCETYPE_KUBEVIRT_IO_DEFAULT_INSTANCETYPE"
)

type OCIUploaderOptions struct {
	Name               string
	Namespace          string
	ServiceAccountName string

	ExportServerUrl         string
	ExportServerToken       string
	ExportServerCertificate string

	Image              string
	ImageFormat        string
	RegistryUsername   string
	RegistryPassword   string
	RegistrySecretName string
	RegistryInsecure   bool

	DefaultPreference   string
	DefaultInstanceType string
}

type imageConfig struct {
	Architecture string               `json:"architecture"`
	OS           string               `json:"os"`
	Config       imageContainerConfig `json:"config"`
	RootFS       imageRootFS          `json:"rootfs"`
}

type imageContainerConfig struct {
	Env []string `json:"Env,omitempty"`
}

type imageRootFS struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diff_ids"`
}

// findRegistry returns the key of the registry in a docker config
func findRegistry(image string) string {
	registry, _, found := strings.Cut(image, "/")
	if found && (registry == "localhost" || strings.ContainsAny(registry, ".:")) {
		return registry
	}
	return dockerHubRegistry
}

func buildOCIJobSecretName(name string) string {
	return fmt.Sprintf("%s-%s", name, ociJobSecretSuffix)
}

func GenerateOCIUploaderSecret(job *batchv1.Job, opts OCIUploaderOptions) *corev1.Secret {
	stringData := map[string]string{
		exportTokenEnvVar:   opts.ExportServerToken,
		exportServerPEMCert: opts.ExportServerCertificate,
	}
	if opts.RegistryUsername != "" && opts.RegistryPassword != "" {
		credentials := fmt.Sprintf("%s:%s", opts.RegistryUsername, opts.RegistryPassword)
		dockerConfig, _ := json.Marshal(map[string]interface{}{
			"auths": map[string]interface{}{
				findRegistry(opts.Image): map[string]string{
					"auth": base64.StdEncoding.EncodeToString([]byte(credentials)),
				},
			},
		})
		stringData[corev1.DockerConfigJsonKey] = string(dockerConfig)
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildOCIJobSecretName(opts.Name),
			Namespace: opts.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job")),
			},
		},
		StringData: stringData,
	}
}

func generateImageConfig(opts OCIUploaderOptions) string {
	var env []string
	if opts.DefaultPreference != "" {
		env = append(env, fmt.Sprintf("%s=%s", defaultPreferenceEnvVar, opts.DefaultPreference))
	}
	if opts.DefaultInstanceType != "" {
		env = append(env, fmt.Sprintf("%s=%s", defaultInstanceTypeEnvVar, opts.DefaultInstanceType))
	}

	config, _ := json.Marshal(imageConfig{
		Architecture: architecturePlaceholder,
		OS:           "linux",
		Config:       imageContainerConfig{Env: env},
		RootFS:       imageRootFS{Type: "layers", DiffIDs: []string{"sha256:" + layerDigestPlaceholder}},
	})
	return string(config)
}

// generateConvertScript builds the image archive pushed by the job, with the disk as its only layer
func generateConvertScript(opts OCIUploaderOptions, downloadedFilename string) string {
	downloadedFile := path.Join(tempVolumeMountPath, downloadedFilename)
	diskDirectory := path.Join(tempVolumeMountPath, "disk")
	imageDirectory := path.Join(tempVolumeMountPath, "image")

	prepareDisk := fmt.Sprintf("mv %s %s", downloadedFile, path.Join(diskDirectory, downloadedFilename))
	if opts.ImageFormat != "raw" {
		diskFile := path.Join(diskDirectory, fmt.Sprintf("%s.%s", opts.Name, opts.ImageFormat))
		convertCommand := strings.Join(generateConvertCommand(opts.ImageFormat, downloadedFile, diskFile), " ")
		prepareDisk = fmt.Sprintf("%s\nrm %s", convertCommand, downloadedFile)
	}

	return strings.Join([]string{
		"set -e",
		fmt.Sprintf("mkdir %s %s", diskDirectory, imageDirectory),
		prepareDisk,
		fmt.Sprintf("tar --create --file %s/layer.tar --directory %s --owner 107 --group 107 disk", imageDirectory, tempVolumeMountPath),
		fmt.Sprintf("rm -r %s", diskDirectory),
		"case $(uname -m) in x86_64) architecture=amd64 ;; aarch64) architecture=arm64 ;; *) architecture=$(uname -m) ;; esac",
		fmt.Sprintf("layer_digest=$(sha256sum %s/layer.tar | cut -d ' ' -f 1)", imageDirectory),
		fmt.Sprintf("printf '%%s' \"$%s\" | sed -e \"s/%s/$architecture/\" -e \"s/%s/$layer_digest/\" > %s/config.json",
			imageConfigEnvVar, architecturePlaceholder, layerDigestPlaceholder, imageDirectory),
		fmt.Sprintf("echo '[{\"Config\":\"config.json\",\"Layers\":[\"layer.tar\"]}]' > %s/manifest.json", imageDirectory),
		fmt.Sprintf("tar --create --file %s --directory %s manifest.json config.json layer.tar",
			path.Join(tempVolumeMountPath, imageArchiveFilename), imageDirectory),
	}, "\n")
}

func GenerateOCIUploaderJob(export *exportv1.VirtualMachineExport, opts OCIUploaderOptions) *batchv1.Job {
	downloadedFilename := fmt.Sprintf("%s.img", opts.Name)

	pushCommand := []string{"krane", "push", path.Join(tempVolumeMountPath, imageArchiveFilename), opts.Image}
	if opts.RegistryInsecure {
		pushCommand = append(pushCommand, "--insecure")
	}

	volumes := []corev1.Volume{
		{
			Name: tempVolumeMountVolumeMapping,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: certVolumeMountVolumeMapping,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: buildOCIJobSecretName(opts.Name),
					Items: []corev1.KeyToPath{
						{
							Key:  exportServerPEMCert,
							Path: exportServerPEMCert,
						},
					},
				},
			},
		},
	}
	pushVolumeMounts := []corev1.VolumeMount{
		{
			Name:      tempVolumeMountVolumeMapping,
			MountPath: tempVolumeMountPath,
		},
	}
	var pushEnv []corev1.EnvVar

	dockerConfigSecretName := opts.RegistrySecretName
	if opts.RegistryUsername != "" && opts.RegistryPassword != "" {
		dockerConfigSecretName = buildOCIJobSecretName(opts.Name)
	}
	if dockerConfigSecretName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: dockerConfigVolumeMountVolumeMapping,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: dockerConfigSecretName,
					Items: []corev1.KeyToPath{
						{
							Key:  corev1.DockerConfigJsonKey,
							Path: dockerConfigFilename,
						},
					},
				},
			},
		})
		pushVolumeMounts = append(pushVolumeMounts, corev1.VolumeMount{
			Name:      dockerConfigVolumeMountVolumeMapping,
			MountPath: dockerConfigVolumeMountPath,
		})
		pushEnv = append(pushEnv, corev1.EnvVar{
			Name:  "DOCKER_CONFIG",
			Value: dockerConfigVolumeMountPath,
		})
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("oci-uploader-%s", opts.Name),
			Namespace: opts.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(export, exportv1.SchemeGroupVersion.WithKind(k8s.VirtualMachineExportKind)),
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: opts.ServiceAccountName,
					InitContainers: []corev1.Container{
						generateDownloadContainer(buildOCIJobSecretName(opts.Name), downloadedFilename, opts.ExportServerUrl, opts.ImageFormat != ""),
						generateConvertContainer(
							[]string{"/bin/sh", "-c", generateConvertScript(opts, downloadedFilename)},
							[]corev1.EnvVar{{Name: imageConfigEnvVar, Value: generateImageConfig(opts)}},
						),
					},
					Containers: []corev1.Container{
						{
							Name:         "push",
							Image:        pushImage,
							Command:      pushCommand,
							Env:          pushEnv,
							VolumeMounts: pushVolumeMounts,
						},
					},
					Volumes:       volumes,
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
}

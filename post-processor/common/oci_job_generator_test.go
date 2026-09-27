package common

import (
	"encoding/base64"
	"encoding/json"
	corev1 "k8s.io/api/core/v1"
	"slices"
	"strings"
	"testing"
)

func newOCIUploaderOptions() OCIUploaderOptions {
	return OCIUploaderOptions{
		Name:        "base-ubuntu",
		Namespace:   "packer",
		Image:       "ghcr.io/pelotech/base-ubuntu:22.04",
		ImageFormat: "qcow2",
	}
}

func findDockerConfigSecretName(podSpec corev1.PodSpec) string {
	for _, volume := range podSpec.Volumes {
		if volume.Name == dockerConfigVolumeMountVolumeMapping {
			return volume.Secret.SecretName
		}
	}
	return ""
}

func readImageEnv(t *testing.T, podSpec corev1.PodSpec) []string {
	t.Helper()
	for _, env := range podSpec.InitContainers[1].Env {
		if env.Name == imageConfigEnvVar {
			var imageConfig struct {
				Config struct {
					Env []string
				}
			}
			if err := json.Unmarshal([]byte(env.Value), &imageConfig); err != nil {
				t.Fatalf("expected a JSON image config, got: %s", env.Value)
			}
			return imageConfig.Config.Env
		}
	}
	t.Fatal("expected the image config in the environment of the convert container")
	return nil
}

func TestGenerateOCIUploaderJobWithQcow2(t *testing.T) {
	podSpec := GenerateOCIUploaderJob(newExport(), newOCIUploaderOptions()).Spec.Template.Spec

	if len(podSpec.InitContainers) != 2 || podSpec.InitContainers[0].Name != "download" || podSpec.InitContainers[1].Name != "convert" {
		t.Fatalf("expected the download then convert init containers, got: %v", podSpec.InitContainers)
	}
	if download := strings.Join(podSpec.InitContainers[0].Command, " "); !strings.Contains(download, "-o /tmp/base-ubuntu.img ") {
		t.Errorf("expected the raw image to be downloaded, got: %s", download)
	}
	convert := strings.Join(podSpec.InitContainers[1].Command, " ")
	if !strings.Contains(convert, "qemu-img convert -f raw -O qcow2 /tmp/base-ubuntu.img /tmp/disk/base-ubuntu.qcow2") {
		t.Errorf("expected the image to be converted into the disk directory, got: %s", convert)
	}
	if !strings.Contains(convert, "--directory /tmp --owner 107 --group 107 disk") {
		t.Errorf("expected a layer with the disk directory owned by 107, got: %s", convert)
	}
	expectedPush := "krane push /tmp/image.tar ghcr.io/pelotech/base-ubuntu:22.04"
	if push := strings.Join(podSpec.Containers[0].Command, " "); push != expectedPush {
		t.Errorf("expected push command '%s', got: '%s'", expectedPush, push)
	}
}

func TestGenerateOCIUploaderJobWithRaw(t *testing.T) {
	opts := newOCIUploaderOptions()
	opts.ImageFormat = "raw"

	podSpec := GenerateOCIUploaderJob(newExport(), opts).Spec.Template.Spec

	convert := strings.Join(podSpec.InitContainers[1].Command, " ")
	if strings.Contains(convert, "qemu-img") {
		t.Errorf("expected no conversion, got: %s", convert)
	}
	if !strings.Contains(convert, "mv /tmp/base-ubuntu.img /tmp/disk/base-ubuntu.img") {
		t.Errorf("expected the raw image to be moved into the disk directory, got: %s", convert)
	}
}

func TestGenerateOCIUploaderJobWithInsecureRegistry(t *testing.T) {
	opts := newOCIUploaderOptions()
	opts.RegistryInsecure = true

	podSpec := GenerateOCIUploaderJob(newExport(), opts).Spec.Template.Spec

	if push := podSpec.Containers[0].Command; !slices.Contains(push, "--insecure") {
		t.Errorf("expected the insecure flag in the push command, got: %v", push)
	}
}

func TestGenerateOCIUploaderJobWithRegistryCredentials(t *testing.T) {
	opts := newOCIUploaderOptions()
	opts.RegistryUsername = "packer"
	opts.RegistryPassword = "secret"

	job := GenerateOCIUploaderJob(newExport(), opts)
	secret := GenerateOCIUploaderSecret(job, opts)

	if name := findDockerConfigSecretName(job.Spec.Template.Spec); name != secret.Name {
		t.Errorf("expected the docker config of the secret '%s', got: '%s'", secret.Name, name)
	}
	var dockerConfig struct {
		Auths map[string]struct {
			Auth string
		}
	}
	if err := json.Unmarshal([]byte(secret.StringData[corev1.DockerConfigJsonKey]), &dockerConfig); err != nil {
		t.Fatalf("expected a JSON docker config, got: %v", secret.StringData)
	}
	expectedAuth := base64.StdEncoding.EncodeToString([]byte("packer:secret"))
	if auth := dockerConfig.Auths["ghcr.io"].Auth; auth != expectedAuth {
		t.Errorf("expected credentials for 'ghcr.io', got: %v", dockerConfig.Auths)
	}
}

func TestGenerateOCIUploaderJobWithRegistrySecretName(t *testing.T) {
	opts := newOCIUploaderOptions()
	opts.RegistrySecretName = "registry-credentials"

	job := GenerateOCIUploaderJob(newExport(), opts)
	podSpec := job.Spec.Template.Spec

	if name := findDockerConfigSecretName(podSpec); name != "registry-credentials" {
		t.Errorf("expected the docker config of the secret 'registry-credentials', got: '%s'", name)
	}
	expectedEnv := corev1.EnvVar{Name: "DOCKER_CONFIG", Value: dockerConfigVolumeMountPath}
	if env := podSpec.Containers[0].Env; !slices.Contains(env, expectedEnv) {
		t.Errorf("expected the push container to read the docker config, got: %v", env)
	}
	secret := GenerateOCIUploaderSecret(job, opts)
	if _, found := secret.StringData[corev1.DockerConfigJsonKey]; found {
		t.Errorf("expected no docker config in the secret, got keys: %v", secret.StringData)
	}
}

func TestGenerateOCIUploaderJobWithoutRegistryCredentials(t *testing.T) {
	opts := newOCIUploaderOptions()
	opts.ServiceAccountName = "oci-uploader"

	job := GenerateOCIUploaderJob(newExport(), opts)
	podSpec := job.Spec.Template.Spec

	if name := podSpec.ServiceAccountName; name != "oci-uploader" {
		t.Errorf("expected service account 'oci-uploader', got: '%s'", name)
	}
	if name := findDockerConfigSecretName(podSpec); name != "" {
		t.Errorf("expected no docker config, got the one of the secret '%s'", name)
	}
	if env := podSpec.Containers[0].Env; len(env) != 0 {
		t.Errorf("expected no environment for the push container, got: %v", env)
	}
	secret := GenerateOCIUploaderSecret(job, opts)
	if _, found := secret.StringData[corev1.DockerConfigJsonKey]; found {
		t.Errorf("expected no docker config in the secret, got keys: %v", secret.StringData)
	}
}

func TestGenerateOCIUploaderJobWithDefaults(t *testing.T) {
	opts := newOCIUploaderOptions()
	opts.DefaultPreference = "ubuntu"
	opts.DefaultInstanceType = "u1.medium"

	env := readImageEnv(t, GenerateOCIUploaderJob(newExport(), opts).Spec.Template.Spec)

	expectedEnv := []string{
		"INSTANCETYPE_KUBEVIRT_IO_DEFAULT_PREFERENCE=ubuntu",
		"INSTANCETYPE_KUBEVIRT_IO_DEFAULT_INSTANCETYPE=u1.medium",
	}
	if !slices.Equal(env, expectedEnv) {
		t.Errorf("expected image environment %v, got: %v", expectedEnv, env)
	}
}

func TestGenerateOCIUploaderJobWithoutDefaults(t *testing.T) {
	opts := newOCIUploaderOptions()

	if env := readImageEnv(t, GenerateOCIUploaderJob(newExport(), opts).Spec.Template.Spec); len(env) != 0 {
		t.Errorf("expected no image environment, got: %v", env)
	}

	opts.DefaultPreference = "ubuntu"
	expectedEnv := []string{"INSTANCETYPE_KUBEVIRT_IO_DEFAULT_PREFERENCE=ubuntu"}
	if env := readImageEnv(t, GenerateOCIUploaderJob(newExport(), opts).Spec.Template.Spec); !slices.Equal(env, expectedEnv) {
		t.Errorf("expected image environment %v, got: %v", expectedEnv, env)
	}

	opts.DefaultPreference, opts.DefaultInstanceType = "", "u1.medium"
	expectedEnv = []string{"INSTANCETYPE_KUBEVIRT_IO_DEFAULT_INSTANCETYPE=u1.medium"}
	if env := readImageEnv(t, GenerateOCIUploaderJob(newExport(), opts).Spec.Template.Spec); !slices.Equal(env, expectedEnv) {
		t.Errorf("expected image environment %v, got: %v", expectedEnv, env)
	}
}

func TestFindRegistry(t *testing.T) {
	registries := map[string]string{
		"ghcr.io/pelotech/base-ubuntu:22.04":   "ghcr.io",
		"registry:5000/base-ubuntu:22.04":      "registry:5000",
		"localhost/pelotech/base-ubuntu:22.04": "localhost",
		"pelotech/base-ubuntu:22.04":           "https://index.docker.io/v1/",
		"base-ubuntu:22.04":                    "https://index.docker.io/v1/",
	}
	for image, expectedRegistry := range registries {
		if registry := findRegistry(image); registry != expectedRegistry {
			t.Errorf("expected registry '%s' for image '%s', got: '%s'", expectedRegistry, image, registry)
		}
	}
}

func TestGenerateOCIUploaderSecretWithExportToken(t *testing.T) {
	opts := newOCIUploaderOptions()
	opts.ExportServerToken = "token"

	job := GenerateOCIUploaderJob(newExport(), opts)
	secret := GenerateOCIUploaderSecret(job, opts)

	download := job.Spec.Template.Spec.InitContainers[0]
	if name := download.Env[0].ValueFrom.SecretKeyRef.Name; name != secret.Name {
		t.Errorf("expected the download container to read the token of the secret '%s', got: '%s'", secret.Name, name)
	}
	if secret.StringData[exportTokenEnvVar] != "token" {
		t.Errorf("expected the export token in the secret, got keys: %v", secret.StringData)
	}
}

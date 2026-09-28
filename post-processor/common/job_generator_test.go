package common

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	exportv1 "kubevirt.io/api/export/v1"
	"strings"
	"testing"
)

func newExport() *exportv1.VirtualMachineExport {
	return &exportv1.VirtualMachineExport{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
	}
}

func findSecretNames(podSpec corev1.PodSpec) []string {
	var names []string
	for _, container := range append(podSpec.InitContainers, podSpec.Containers...) {
		for _, env := range container.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
				names = append(names, env.ValueFrom.SecretKeyRef.Name)
			}
		}
		for _, envFrom := range container.EnvFrom {
			if envFrom.SecretRef != nil {
				names = append(names, envFrom.SecretRef.Name)
			}
		}
	}
	for _, volume := range podSpec.Volumes {
		if volume.Secret != nil {
			names = append(names, volume.Secret.SecretName)
		}
	}
	return names
}

// checkJobsOfTheSameExport checks that two post-processors of the same type can run on one export
func checkJobsOfTheSameExport(t *testing.T, prefix string, jobs [2]*batchv1.Job, secrets [2]*corev1.Secret) {
	t.Helper()
	if jobs[0].Name == jobs[1].Name {
		t.Errorf("expected each job to have its own name, got '%s' twice", jobs[0].Name)
	}
	if secrets[0].Name == secrets[1].Name {
		t.Errorf("expected each secret to have its own name, got '%s' twice", secrets[0].Name)
	}
	for index, job := range jobs {
		if !strings.HasPrefix(job.Name, prefix) {
			t.Errorf("expected the job name to start with '%s', got: '%s'", prefix, job.Name)
		}
		for _, name := range findSecretNames(job.Spec.Template.Spec) {
			if name != secrets[index].Name {
				t.Errorf("expected the job '%s' to read its secret '%s', got: '%s'", job.Name, secrets[index].Name, name)
			}
		}
	}
}

func TestGenerateS3UploaderJobsOfTheSameExport(t *testing.T) {
	opts := S3UploaderOptions{Name: "base-ubuntu", Namespace: "packer"}

	jobs := [2]*batchv1.Job{GenerateS3UploaderJob(newExport(), opts), GenerateS3UploaderJob(newExport(), opts)}
	secrets := [2]*corev1.Secret{GenerateS3UploaderSecret(jobs[0], opts), GenerateS3UploaderSecret(jobs[1], opts)}

	checkJobsOfTheSameExport(t, "s3-uploader-base-ubuntu-", jobs, secrets)
}

func TestGenerateUploaderJobsWithALongExportName(t *testing.T) {
	export := newExport()
	export.Name = strings.Repeat("a", validation.DNS1123LabelMaxLength)

	for _, job := range []*batchv1.Job{
		GenerateS3UploaderJob(export, S3UploaderOptions{Name: export.Name, Namespace: "packer"}),
		GenerateOCIUploaderJob(export, OCIUploaderOptions{Name: export.Name, Namespace: "packer"}),
	} {
		// the job name ends up in a label of its pods
		if problems := validation.IsValidLabelValue(job.Name); len(problems) > 0 {
			t.Errorf("expected the job name '%s' to be a valid label value, got: %v", job.Name, problems)
		}
	}
}

func TestGenerateUploaderJobsAreRetriedOnce(t *testing.T) {
	for _, job := range []*batchv1.Job{
		GenerateS3UploaderJob(newExport(), S3UploaderOptions{Name: "base-ubuntu", Namespace: "packer"}),
		GenerateOCIUploaderJob(newExport(), newOCIUploaderOptions()),
	} {
		// Kubernetes retries a job 6 times when its backoff limit is unset
		if retries := ptr.Deref(job.Spec.BackoffLimit, 6); retries != 1 {
			t.Errorf("expected the job '%s' to be retried once, got %d retries", job.Name, retries)
		}
	}
}

func TestGenerateS3UploaderJobIsOwnedByExport(t *testing.T) {
	export := newExport()
	export.UID = "export-uid"

	job := GenerateS3UploaderJob(export, S3UploaderOptions{Name: "base-ubuntu", Namespace: "packer"})

	owner := metav1.GetControllerOf(job)
	if owner == nil || owner.APIVersion != "export.kubevirt.io/v1" || owner.Kind != "VirtualMachineExport" || owner.UID != export.UID {
		t.Errorf("expected the job to be owned by the 'v1' export, got: %v", job.OwnerReferences)
	}
}

func TestGenerateS3UploaderJobWithStaticCredentials(t *testing.T) {
	accessKeyId, secretAccessKey := "access-key-id", "secret-access-key"
	opts := S3UploaderOptions{
		Name:               "base-ubuntu",
		Namespace:          "packer",
		AWSAccessKeyId:     accessKeyId,
		AWSSecretAccessKey: secretAccessKey,
	}

	job := GenerateS3UploaderJob(newExport(), opts)
	if name := job.Spec.Template.Spec.ServiceAccountName; name != "" {
		t.Errorf("expected no service account, got: '%s'", name)
	}

	secret := GenerateS3UploaderSecret(job, opts)
	if secret.StringData["AWS_ACCESS_KEY_ID"] != accessKeyId || secret.StringData["AWS_SECRET_ACCESS_KEY"] != secretAccessKey {
		t.Errorf("expected AWS credentials in the secret, got keys: %v", secret.StringData)
	}
}

func TestGenerateS3UploaderJobWithServiceAccount(t *testing.T) {
	serviceAccountName := "s3-uploader"
	opts := S3UploaderOptions{
		Name:               "base-ubuntu",
		Namespace:          "packer",
		ServiceAccountName: serviceAccountName,
	}

	job := GenerateS3UploaderJob(newExport(), opts)
	if name := job.Spec.Template.Spec.ServiceAccountName; name != serviceAccountName {
		t.Errorf("expected service account '%s', got: '%s'", serviceAccountName, name)
	}

	secret := GenerateS3UploaderSecret(job, opts)
	if _, found := secret.StringData["AWS_ACCESS_KEY_ID"]; found {
		t.Errorf("expected no AWS credentials in the secret, got keys: %v", secret.StringData)
	}
}

func TestGenerateS3UploaderSecretWithEndpointUrl(t *testing.T) {
	opts := S3UploaderOptions{
		Name:          "base-ubuntu",
		Namespace:     "packer",
		S3EndpointUrl: "http://garage.garage.svc:3900",
	}
	job := GenerateS3UploaderJob(newExport(), opts)

	secret := GenerateS3UploaderSecret(job, opts)
	if url := secret.StringData["AWS_ENDPOINT_URL"]; url != opts.S3EndpointUrl {
		t.Errorf("expected endpoint URL '%s' in the secret, got: '%s'", opts.S3EndpointUrl, url)
	}

	opts.S3EndpointUrl = ""
	secret = GenerateS3UploaderSecret(job, opts)
	if _, found := secret.StringData["AWS_ENDPOINT_URL"]; found {
		t.Errorf("expected no endpoint URL in the secret, got keys: %v", secret.StringData)
	}
}

func TestGenerateS3UploaderJobWithoutImageFormat(t *testing.T) {
	serviceAccountName := "s3-uploader"
	opts := S3UploaderOptions{
		Name:               "base-ubuntu",
		Namespace:          "packer",
		ServiceAccountName: serviceAccountName,
		S3BucketName:       "images",
		S3KeyPrefix:        "exports",
	}

	podSpec := GenerateS3UploaderJob(newExport(), opts).Spec.Template.Spec

	if len(podSpec.InitContainers) != 1 || podSpec.InitContainers[0].Name != "download" {
		t.Fatalf("expected the download init container only, got: %v", podSpec.InitContainers)
	}
	if download := strings.Join(podSpec.InitContainers[0].Command, " "); !strings.Contains(download, "-o /tmp/base-ubuntu.img.gz ") {
		t.Errorf("expected the compressed image to be downloaded as it is, got: %s", download)
	}
	if upload := strings.Join(podSpec.Containers[0].Command, " "); !strings.HasSuffix(upload, "/tmp/base-ubuntu.img.gz s3://images/exports/base-ubuntu.img.gz") {
		t.Errorf("expected the compressed raw image to be uploaded, got: %s", upload)
	}
}

func TestGenerateS3UploaderJobDownloadFailsOnHTTPError(t *testing.T) {
	for name, imageFormat := range map[string]string{
		"compressed raw image": "",
		"converted image":      "qcow2",
	} {
		t.Run(name, func(t *testing.T) {
			opts := S3UploaderOptions{Name: "base-ubuntu", Namespace: "packer", ImageFormat: imageFormat}

			podSpec := GenerateS3UploaderJob(newExport(), opts).Spec.Template.Spec
			// otherwise curl saves the error page of the export server as the image and exits 0
			if download := strings.Join(podSpec.InitContainers[0].Command, " "); !strings.Contains(download, "curl --fail ") {
				t.Errorf("expected the download to fail on an HTTP error, got: %s", download)
			}
		})
	}
}

func TestGenerateS3UploaderJobWithImageFormat(t *testing.T) {
	serviceAccountName := "s3-uploader"
	opts := S3UploaderOptions{
		Name:               "base-ubuntu",
		Namespace:          "packer",
		ServiceAccountName: serviceAccountName,
		S3BucketName:       "images",
		S3KeyPrefix:        "exports",
		ImageFormat:        "qcow2",
	}

	podSpec := GenerateS3UploaderJob(newExport(), opts).Spec.Template.Spec

	if len(podSpec.InitContainers) != 2 || podSpec.InitContainers[0].Name != "download" || podSpec.InitContainers[1].Name != "convert" {
		t.Fatalf("expected the download then convert init containers, got: %v", podSpec.InitContainers)
	}
	// a raw image has the size of the disk, its empty blocks must not fill the node
	if download := strings.Join(podSpec.InitContainers[0].Command, " "); !strings.Contains(download, "| dd of=/tmp/base-ubuntu.img conv=sparse") {
		t.Errorf("expected the raw image to be downloaded as a sparse file, got: %s", download)
	}
	// the raw image is removed once converted, to leave its space to the upload
	expectedConvert := "/bin/sh -c qemu-img convert -f raw -O qcow2 /tmp/base-ubuntu.img /tmp/base-ubuntu.qcow2 && rm /tmp/base-ubuntu.img"
	if convert := strings.Join(podSpec.InitContainers[1].Command, " "); convert != expectedConvert {
		t.Errorf("expected convert command '%s', got: '%s'", expectedConvert, convert)
	}
	if upload := strings.Join(podSpec.Containers[0].Command, " "); !strings.HasSuffix(upload, "/tmp/base-ubuntu.qcow2 s3://images/exports/base-ubuntu.qcow2") {
		t.Errorf("expected the converted image to be uploaded, got: %s", upload)
	}
}

func TestFindVolumeUrl(t *testing.T) {
	export := newExport()
	export.Status = &exportv1.VirtualMachineExportStatus{
		Links: &exportv1.VirtualMachineExportLinks{
			Internal: &exportv1.VirtualMachineExportLink{
				Volumes: []exportv1.VirtualMachineExportVolume{
					{
						Name: "base-ubuntu-source",
						Formats: []exportv1.VirtualMachineExportVolumeFormat{
							{Format: exportv1.KubeVirtRaw, Url: "https://export/disk.img"},
							{Format: exportv1.KubeVirtGz, Url: "https://export/disk.img.gz"},
						},
					},
				},
			},
		},
	}

	if url := FindVolumeUrl(export, ""); url != "https://export/disk.img.gz" {
		t.Errorf("expected the compressed image URL without image format, got: '%s'", url)
	}
	if url := FindVolumeUrl(export, "qcow2"); url != "https://export/disk.img" {
		t.Errorf("expected the raw image URL with an image format, got: '%s'", url)
	}
	if url := FindVolumeUrl(newExport(), "qcow2"); url != "" {
		t.Errorf("expected no URL for an export without links, got: '%s'", url)
	}
}

func TestValidateImageFormat(t *testing.T) {
	for _, format := range []string{"", "qcow2", "vmdk", "vhdx", "vdi"} {
		if err := ValidateImageFormat(format); err != nil {
			t.Errorf("expected image format '%s' to be valid, got: %v", format, err)
		}
	}
	if err := ValidateImageFormat("iso"); err == nil {
		t.Error("expected image format 'iso' to be rejected")
	}
}

func TestGenerateS3UploaderJobWithObjectName(t *testing.T) {
	for name, test := range map[string]struct {
		imageFormat string
		expected    string
	}{
		"converted image":      {imageFormat: "qcow2", expected: "/tmp/base-ubuntu.qcow2 s3://images/exports/Ubuntu_26.04_en-us_x64+1.0.0.qcow2"},
		"compressed raw image": {imageFormat: "", expected: "/tmp/base-ubuntu.img.gz s3://images/exports/Ubuntu_26.04_en-us_x64+1.0.0.img.gz"},
	} {
		t.Run(name, func(t *testing.T) {
			opts := S3UploaderOptions{
				Name:         "base-ubuntu",
				Namespace:    "packer",
				S3BucketName: "images",
				S3KeyPrefix:  "exports",
				ObjectName:   "Ubuntu_26.04_en-us_x64+1.0.0",
				ImageFormat:  test.imageFormat,
			}

			podSpec := GenerateS3UploaderJob(newExport(), opts).Spec.Template.Spec
			if upload := strings.Join(podSpec.Containers[0].Command, " "); !strings.HasSuffix(upload, test.expected) {
				t.Errorf("expected the image to be uploaded as '%s', got: %s", test.expected, upload)
			}
		})
	}
}

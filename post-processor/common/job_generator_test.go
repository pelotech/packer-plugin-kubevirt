package common

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	exportv1 "kubevirt.io/api/export/v1beta1"
	"strings"
	"testing"
)

func newExport() *exportv1.VirtualMachineExport {
	return &exportv1.VirtualMachineExport{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
	}
}

func TestGenerateS3UploaderJobWithStaticCredentials(t *testing.T) {
	accessKeyId, secretAccessKey := "access-key-id", "secret-access-key"
	opts := S3UploaderOptions{
		Name:               "base-ubuntu",
		Namespace:          "packer",
		AWSAccessKeyId:     &accessKeyId,
		AWSSecretAccessKey: &secretAccessKey,
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
		ServiceAccountName: &serviceAccountName,
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

func TestGenerateS3UploaderJobWithoutImageFormat(t *testing.T) {
	serviceAccountName := "s3-uploader"
	opts := S3UploaderOptions{
		Name:               "base-ubuntu",
		Namespace:          "packer",
		ServiceAccountName: &serviceAccountName,
		S3BucketName:       "images",
		S3KeyPrefix:        "exports",
	}

	podSpec := GenerateS3UploaderJob(newExport(), opts).Spec.Template.Spec

	if len(podSpec.InitContainers) != 1 || podSpec.InitContainers[0].Name != "download" {
		t.Fatalf("expected the download init container only, got: %v", podSpec.InitContainers)
	}
	if upload := strings.Join(podSpec.Containers[0].Command, " "); !strings.HasSuffix(upload, "/tmp/base-ubuntu.img.gz s3://images/exports/base-ubuntu.img.gz") {
		t.Errorf("expected the compressed raw image to be uploaded, got: %s", upload)
	}
}

func TestGenerateS3UploaderJobWithImageFormat(t *testing.T) {
	serviceAccountName := "s3-uploader"
	opts := S3UploaderOptions{
		Name:               "base-ubuntu",
		Namespace:          "packer",
		ServiceAccountName: &serviceAccountName,
		S3BucketName:       "images",
		S3KeyPrefix:        "exports",
		ImageFormat:        "qcow2",
	}

	podSpec := GenerateS3UploaderJob(newExport(), opts).Spec.Template.Spec

	if len(podSpec.InitContainers) != 2 || podSpec.InitContainers[0].Name != "download" || podSpec.InitContainers[1].Name != "convert" {
		t.Fatalf("expected the download then convert init containers, got: %v", podSpec.InitContainers)
	}
	if download := strings.Join(podSpec.InitContainers[0].Command, " "); !strings.Contains(download, "-o /tmp/base-ubuntu.img ") {
		t.Errorf("expected the raw image to be downloaded, got: %s", download)
	}
	expectedConvert := "qemu-img convert -f raw -O qcow2 /tmp/base-ubuntu.img /tmp/base-ubuntu.qcow2"
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

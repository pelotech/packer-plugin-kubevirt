package common

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	exportv1 "kubevirt.io/api/export/v1"
	"strings"
	"testing"
)

func newExport() *exportv1.VirtualMachineExport {
	return &exportv1.VirtualMachineExport{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
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
		ServiceAccountName: &serviceAccountName,
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
	// a raw image has the size of the disk, its empty blocks must not fill the node
	if download := strings.Join(podSpec.InitContainers[0].Command, " "); !strings.Contains(download, "| dd of=/tmp/base-ubuntu.img conv=sparse") {
		t.Errorf("expected the raw image to be downloaded as a sparse file, got: %s", download)
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

package common

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	exportv1 "kubevirt.io/api/export/v1beta1"
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

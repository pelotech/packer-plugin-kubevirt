package common

import (
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"packer-plugin-kubevirt/builder/common/steps"
	"path"
)

const (
	TempVolumeMountVolumeMapping = "temp"
	TempVolumeMountPath          = "/tmp"
)

type S3UploaderOptions struct {
	Name      string
	Namespace string

	ExportServerUrl   string
	ExportServerToken string

	S3BucketName string
	S3Key        string

	AWSAccessKeyId     string
	AWSSecretAccessKey string
	AWSRegion          string
}

func GenerateS3UploaderSecret(opts S3UploaderOptions) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.Name,
			Namespace: opts.Namespace,
		},
		StringData: map[string]string{
			"AWS_ACCESS_KEY_ID":     opts.AWSAccessKeyId,
			"AWS_SECRET_ACCESS_KEY": opts.AWSSecretAccessKey,
			"AWS_REGION":            opts.AWSRegion,
		},
	}
}

func GenerateS3UploaderJob(opts S3UploaderOptions) *batchv1.Job {
	filename := fmt.Sprintf("%s.img.gz", opts.Name)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("s3-uploader-%s", opts.Name),
			Namespace: opts.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "download-vm",
							Image: "busybox",
							Command: []string{
								"/bin/sh",
								"-c",
								fmt.Sprintf("wget --no-check-certificate --header='%s: %s' -O %s/%s %s", steps.ExportTokenHeader, opts.ExportServerToken, TempVolumeMountPath, filename, opts.ExportServerUrl),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      TempVolumeMountVolumeMapping,
									MountPath: TempVolumeMountPath,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "aws-cli",
							Image: "amazon/aws-cli:latest",
							Command: []string{
								"/bin/sh",
								"-c",
								fmt.Sprintf("aws s3 cp %s/%s s3://%s", TempVolumeMountPath, filename, path.Join(opts.S3BucketName, opts.S3Key)),
							},
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: opts.Name,
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      TempVolumeMountVolumeMapping,
									MountPath: TempVolumeMountPath,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: TempVolumeMountVolumeMapping,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
}

package common

import (
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kubevirt.io/api/export/v1alpha1"
	exportv1 "kubevirt.io/api/export/v1alpha1"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/steps"
	"path"
)

const (
	tempVolumeMountVolumeMapping = "temp"
	tempVolumeMountPath          = "/tmp"
	exportTokenEnvVar            = "EXPORT_TOKEN"
	jobSecretSuffix              = "s3-uploader"
)

type S3UploaderOptions struct {
	Name      string
	Namespace string

	ExportServerUrl   string
	ExportServerToken string

	S3BucketName string
	S3KeyPrefix  string

	AWSAccessKeyId     string
	AWSSecretAccessKey string
	AWSRegion          string
}

func GenerateS3UploaderSecret(job *batchv1.Job, opts S3UploaderOptions) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildJobSecretName(opts.Name),
			Namespace: opts.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job")),
			},
		},
		StringData: map[string]string{
			"AWS_ACCESS_KEY_ID":     opts.AWSAccessKeyId,
			"AWS_SECRET_ACCESS_KEY": opts.AWSSecretAccessKey,
			"AWS_REGION":            opts.AWSRegion,
			exportTokenEnvVar:       opts.ExportServerToken,
		},
	}
}

func buildJobSecretName(name string) string {
	return fmt.Sprintf("%s-%s", name, jobSecretSuffix)
}

func GenerateS3UploaderJob(export *v1alpha1.VirtualMachineExport, opts S3UploaderOptions) *batchv1.Job {
	filename := fmt.Sprintf("%s.img.gz", opts.Name)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("s3-uploader-%s", opts.Name),
			Namespace: opts.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(export, exportv1.SchemeGroupVersion.WithKind(k8s.VirtualMachineExportKind)),
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "download",
							Image: "curlimages/curl:8.6.0",
							Command: []string{
								"/bin/sh",
								"-c",
								// TODO: should NOT ignored with '-k' cert but check properly with '--cert pemFile:secret'
								fmt.Sprintf("curl -k -H \"%s: $%s\" -o %s/%s %s", steps.ExportTokenHeader, exportTokenEnvVar, tempVolumeMountPath, filename, opts.ExportServerUrl),
							},
							Env: []corev1.EnvVar{
								{
									Name: exportTokenEnvVar,
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: buildJobSecretName(opts.Name),
											},
											Key: exportTokenEnvVar,
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      tempVolumeMountVolumeMapping,
									MountPath: tempVolumeMountPath,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "upload",
							Image: "amazon/aws-cli:latest",
							Command: []string{
								"/bin/sh",
								"-c",
								fmt.Sprintf("aws s3 cp %s/%s s3://%s", tempVolumeMountPath, filename, path.Join(opts.S3BucketName, opts.S3KeyPrefix, filename)),
							},
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: buildJobSecretName(opts.Name),
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      tempVolumeMountVolumeMapping,
									MountPath: tempVolumeMountPath,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: tempVolumeMountVolumeMapping,
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

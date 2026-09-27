package common

import (
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	exportv1 "kubevirt.io/api/export/v1"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/k8s/generator"
	"packer-plugin-kubevirt/builder/common/steps"
	"path"
	"slices"
	"strings"
)

const (
	certVolumeMountVolumeMapping = "cert"
	certVolumeMountPath          = "/cert"
	tempVolumeMountVolumeMapping = "temp"
	tempVolumeMountPath          = "/tmp"
	exportTokenEnvVar            = "EXPORT_TOKEN"
	exportServerPEMCert          = "cert.pem"
	jobSecretSuffix              = "s3-uploader"
	qemuImgImage                 = "quay.io/kubevirt/cdi-importer:v1.66.1"
)

var supportedImageFormats = []string{"qcow2", "vmdk", "vhdx", "vdi"}

type S3UploaderOptions struct {
	Name               string
	Namespace          string
	ServiceAccountName *string

	ExportServerUrl         string
	ExportServerToken       string
	ExportServerCertificate string

	S3BucketName string
	S3KeyPrefix  string
	ObjectName   string

	AWSAccessKeyId     *string
	AWSSecretAccessKey *string
	AWSRegion          string
	S3EndpointUrl      string

	ImageFormat string
}

func ValidateImageFormat(format string) error {
	if format != "" && !slices.Contains(supportedImageFormats, format) {
		return fmt.Errorf("unsupported image format '%s', allowed values: %s", format, strings.Join(supportedImageFormats, ", "))
	}
	return nil
}

// FindVolumeUrl returns the raw disk image URL when the image is converted, the compressed one otherwise
func FindVolumeUrl(export *exportv1.VirtualMachineExport, imageFormat string) string {
	if export.Status == nil || export.Status.Links == nil || export.Status.Links.Internal == nil {
		return ""
	}

	exportFormat := exportv1.KubeVirtGz
	if imageFormat != "" {
		exportFormat = exportv1.KubeVirtRaw
	}
	for _, vol := range export.Status.Links.Internal.Volumes {
		if strings.HasSuffix(vol.Name, string(generator.SourceDataVolumeSuffix)) { // may need better logic if many volumes
			for _, volumeFormat := range vol.Formats {
				if volumeFormat.Format == exportFormat {
					return volumeFormat.Url
				}
			}
		}
	}
	return ""
}

func GenerateS3UploaderSecret(job *batchv1.Job, opts S3UploaderOptions) *corev1.Secret {
	stringData := map[string]string{
		"AWS_REGION":        opts.AWSRegion,
		exportTokenEnvVar:   opts.ExportServerToken,
		exportServerPEMCert: opts.ExportServerCertificate,
	}
	if opts.AWSAccessKeyId != nil && opts.AWSSecretAccessKey != nil {
		stringData["AWS_ACCESS_KEY_ID"] = *opts.AWSAccessKeyId
		stringData["AWS_SECRET_ACCESS_KEY"] = *opts.AWSSecretAccessKey
	}
	if opts.S3EndpointUrl != "" {
		stringData["AWS_ENDPOINT_URL"] = opts.S3EndpointUrl
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildJobSecretName(opts.Name),
			Namespace: opts.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job")),
			},
		},
		StringData: stringData,
	}
}

func buildJobSecretName(name string) string {
	return fmt.Sprintf("%s-%s", name, jobSecretSuffix)
}

func generateDownloadContainer(secretName, downloadedFilename, exportServerUrl string, raw bool) corev1.Container {
	image := "curlimages/curl:8.22.0"
	command := []string{
		"/bin/sh",
		"-c",
		fmt.Sprintf("curl --cacert %s/%s -o %s/%s -H \"%s: $%s\" %s",
			certVolumeMountPath, exportServerPEMCert,
			tempVolumeMountPath, downloadedFilename,
			steps.ExportTokenHeader, exportTokenEnvVar,
			exportServerUrl),
	}
	if raw {
		// a raw image has the size of the disk, its empty blocks are left out of the file
		image = qemuImgImage
		command = []string{
			"/bin/bash",
			"-c",
			fmt.Sprintf("set -o pipefail; curl --fail --cacert %s/%s -H \"%s: $%s\" %s | dd of=%s/%s conv=sparse bs=1M",
				certVolumeMountPath, exportServerPEMCert,
				steps.ExportTokenHeader, exportTokenEnvVar,
				exportServerUrl,
				tempVolumeMountPath, downloadedFilename),
		}
	}

	return corev1.Container{
		Name:    "download",
		Image:   image,
		Command: command,
		Env: []corev1.EnvVar{
			{
				Name: exportTokenEnvVar,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: secretName,
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
			{
				Name:      certVolumeMountVolumeMapping,
				MountPath: certVolumeMountPath,
			},
		},
	}
}

func GenerateS3UploaderJob(export *exportv1.VirtualMachineExport, opts S3UploaderOptions) *batchv1.Job {
	downloadedFilename := fmt.Sprintf("%s.img.gz", opts.Name)
	filename := downloadedFilename
	var convertContainers []corev1.Container
	if opts.ImageFormat != "" {
		downloadedFilename = fmt.Sprintf("%s.img", opts.Name)
		filename = fmt.Sprintf("%s.%s", opts.Name, opts.ImageFormat)
		convertContainers = append(convertContainers, corev1.Container{
			Name:  "convert",
			Image: qemuImgImage,
			Command: []string{
				"qemu-img", "convert", "-f", "raw", "-O", opts.ImageFormat,
				path.Join(tempVolumeMountPath, downloadedFilename),
				path.Join(tempVolumeMountPath, filename),
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      tempVolumeMountVolumeMapping,
					MountPath: tempVolumeMountPath,
				},
			},
		})
	}

	objectFilename := filename
	if opts.ObjectName != "" {
		objectFilename = opts.ObjectName + strings.TrimPrefix(filename, opts.Name)
	}

	var serviceAccountName string
	if opts.ServiceAccountName != nil {
		serviceAccountName = *opts.ServiceAccountName
	}

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
					ServiceAccountName: serviceAccountName,
					InitContainers: append([]corev1.Container{
						generateDownloadContainer(buildJobSecretName(opts.Name), downloadedFilename, opts.ExportServerUrl, opts.ImageFormat != ""),
					}, convertContainers...),
					Containers: []corev1.Container{
						{
							Name:  "upload",
							Image: "amazon/aws-cli:2.36.49",
							Command: []string{
								"/bin/sh",
								"-c",
								fmt.Sprintf("aws s3 cp %s/%s s3://%s", tempVolumeMountPath, filename, path.Join(opts.S3BucketName, opts.S3KeyPrefix, objectFilename)),
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
						{
							Name: certVolumeMountVolumeMapping,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: buildJobSecretName(opts.Name),
									Items: []corev1.KeyToPath{
										{
											Key:  exportServerPEMCert,
											Path: exportServerPEMCert,
										},
									},
								},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
}

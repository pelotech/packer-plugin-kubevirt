package common

import (
	"fmt"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/validate/content"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	exportv1 "kubevirt.io/api/export/v1"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/k8s/generator"
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
	exportTokenHeader            = "x-kubevirt-export-token"
	exportServerPEMCert          = "cert.pem"
	qemuImgImage                 = "quay.io/kubevirt/cdi-importer:v1.66.1"
	// the job name ends up in a label of its pods
	jobNameMaxLength    = content.LabelValueMaxLength
	jobNameSuffixLength = 5
)

var supportedImageFormats = []string{"qcow2", "vmdk", "vhdx", "vdi"}

var tempVolumeMount = corev1.VolumeMount{
	Name:      tempVolumeMountVolumeMapping,
	MountPath: tempVolumeMountPath,
}

type S3UploaderOptions struct {
	Name               string
	Namespace          string
	ServiceAccountName string

	ExportServerUrl         string
	ExportServerToken       string
	ExportServerCertificate string

	S3BucketName string
	S3KeyPrefix  string
	ObjectName   string

	AWSAccessKeyId     string
	AWSSecretAccessKey string
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

// downloadedFilename follows FindVolumeUrl: the raw image when it is converted, the compressed one otherwise
func downloadedFilename(name, imageFormat string) string {
	if imageFormat != "" {
		return name + ".img"
	}
	return name + ".img.gz"
}

func GenerateS3UploaderSecret(job *batchv1.Job, opts S3UploaderOptions) *corev1.Secret {
	stringData := map[string]string{
		"AWS_REGION": opts.AWSRegion,
	}
	if opts.AWSAccessKeyId != "" && opts.AWSSecretAccessKey != "" {
		stringData["AWS_ACCESS_KEY_ID"] = opts.AWSAccessKeyId
		stringData["AWS_SECRET_ACCESS_KEY"] = opts.AWSSecretAccessKey
	}
	if opts.S3EndpointUrl != "" {
		stringData["AWS_ENDPOINT_URL"] = opts.S3EndpointUrl
	}

	return generateUploaderSecret(job, opts.ExportServerToken, opts.ExportServerCertificate, stringData)
}

// generateUploaderSecret adds the export token and certificate the download reads to the data of an uploader
func generateUploaderSecret(job *batchv1.Job, token, certificate string, stringData map[string]string) *corev1.Secret {
	stringData[exportTokenEnvVar] = token
	stringData[exportServerPEMCert] = certificate

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      job.Name,
			Namespace: job.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job")),
			},
		},
		StringData: stringData,
	}
}

// generateUploaderJob gives the S3 and OCI uploaders the same pod: its secret has the name of the job
func generateUploaderJob(export *exportv1.VirtualMachineExport, name, namespace, serviceAccountName string, initContainers, containers []corev1.Container, extraVolumes ...corev1.Volume) *batchv1.Job {
	volumes := append([]corev1.Volume{
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
					SecretName: name,
					Items: []corev1.KeyToPath{
						{
							Key:  exportServerPEMCert,
							Path: exportServerPEMCert,
						},
					},
				},
			},
		},
	}, extraVolumes...)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(export, exportv1.SchemeGroupVersion.WithKind(k8s.VirtualMachineExportKind)),
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: serviceAccountName,
					InitContainers:     initContainers,
					Containers:         containers,
					Volumes:            volumes,
					RestartPolicy:      corev1.RestartPolicyNever,
				},
			},
		},
	}
}

// buildJobName adds a random suffix like metadata.generateName does, so that each job of an export has its own name
func buildJobName(prefix, exportName string) string {
	base := fmt.Sprintf("%s-%s-", prefix, exportName)
	if len(base) > jobNameMaxLength-jobNameSuffixLength {
		base = base[:jobNameMaxLength-jobNameSuffixLength]
	}
	return base + rand.String(jobNameSuffixLength)
}

func generateDownloadContainer(secretName, downloadedFilename, exportServerUrl string, raw bool) corev1.Container {
	image := "curlimages/curl:8.22.0"
	command := []string{
		"/bin/sh",
		"-c",
		fmt.Sprintf("curl --fail --cacert %s/%s -o %s/%s -H \"%s: $%s\" %s",
			certVolumeMountPath, exportServerPEMCert,
			tempVolumeMountPath, downloadedFilename,
			exportTokenHeader, exportTokenEnvVar,
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
				exportTokenHeader, exportTokenEnvVar,
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
			tempVolumeMount,
			{
				Name:      certVolumeMountVolumeMapping,
				MountPath: certVolumeMountPath,
			},
		},
	}
}

func generateConvertCommand(imageFormat, downloadedFile, convertedFile string) []string {
	return []string{"qemu-img", "convert", "-f", "raw", "-O", imageFormat, downloadedFile, convertedFile}
}

func generateConvertContainer(command []string, env []corev1.EnvVar) corev1.Container {
	return corev1.Container{
		Name:         "convert",
		Image:        qemuImgImage,
		Command:      command,
		Env:          env,
		VolumeMounts: []corev1.VolumeMount{tempVolumeMount},
	}
}

func GenerateS3UploaderJob(export *exportv1.VirtualMachineExport, opts S3UploaderOptions) *batchv1.Job {
	// its secret has the same name
	jobName := buildJobName("s3-uploader", opts.Name)
	downloaded := downloadedFilename(opts.Name, opts.ImageFormat)

	initContainers := []corev1.Container{
		generateDownloadContainer(jobName, downloaded, opts.ExportServerUrl, opts.ImageFormat != ""),
	}
	filename := downloaded
	if opts.ImageFormat != "" {
		filename = fmt.Sprintf("%s.%s", opts.Name, opts.ImageFormat)
		downloadedFile := path.Join(tempVolumeMountPath, downloaded)
		convertCommand := strings.Join(generateConvertCommand(opts.ImageFormat, downloadedFile, path.Join(tempVolumeMountPath, filename)), " ")
		// the raw image is not uploaded, its space is freed for the upload
		initContainers = append(initContainers, generateConvertContainer([]string{"/bin/sh", "-c", fmt.Sprintf("%s && rm %s", convertCommand, downloadedFile)}, nil))
	}

	objectFilename := filename
	if opts.ObjectName != "" {
		objectFilename = opts.ObjectName + strings.TrimPrefix(filename, opts.Name)
	}

	upload := corev1.Container{
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
						Name: jobName,
					},
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{tempVolumeMount},
	}

	return generateUploaderJob(export, jobName, opts.Namespace, opts.ServiceAccountName, initContainers, []corev1.Container{upload})
}

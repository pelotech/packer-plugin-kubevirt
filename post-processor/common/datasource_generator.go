package common

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	instancetypeapi "kubevirt.io/api/instancetype"
	cdiv1beta1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
	"packer-plugin-kubevirt/builder/common/steps"
	"strings"
	"time"
)

const (
	dataVolumeKind             = "DataVolume"
	immediateBindingAnnotation = "cdi.kubevirt.io/storage.bind.immediate.requested"
	exportTokenHeaderKey       = "token"
	// the importer needs this file name when it reads the image through nbdkit
	exportServerCertificateKey = "tls.crt"
	volumeNameTimeFormat       = "20060102150405"
)

type DataSourceOptions struct {
	Name       string
	Namespace  string
	VolumeName string

	ExportServerUrl         string
	ExportServerToken       string
	ExportServerCertificate string

	VolumeSize   string
	StorageClass string

	DefaultPreference   string
	DefaultInstanceType string
}

func BuildVolumeName(name string, buildTime time.Time) string {
	return fmt.Sprintf("%s-%s", name, buildTime.UTC().Format(volumeNameTimeFormat))
}

// ValidateDataSourceOptions only checks the options which are set
func ValidateDataSourceOptions(opts DataSourceOptions) error {
	if opts.Name != "" {
		volumeName := BuildVolumeName(opts.Name, time.Now())
		if problems := validation.IsDNS1123Subdomain(volumeName); len(problems) > 0 {
			return fmt.Errorf("invalid 'datasource_name' value '%s', the volume would be named '%s': %s", opts.Name, volumeName, strings.Join(problems, ", "))
		}
	}
	if opts.Namespace != "" {
		if problems := validation.IsDNS1123Label(opts.Namespace); len(problems) > 0 {
			return fmt.Errorf("invalid 'datasource_namespace' value '%s': %s", opts.Namespace, strings.Join(problems, ", "))
		}
	}
	if opts.VolumeSize != "" {
		if _, err := resource.ParseQuantity(opts.VolumeSize); err != nil {
			return fmt.Errorf("invalid 'volume_size' value '%s': %s", opts.VolumeSize, err)
		}
	}
	if opts.StorageClass != "" {
		if problems := validation.IsDNS1123Subdomain(opts.StorageClass); len(problems) > 0 {
			return fmt.Errorf("invalid 'volume_storage_class' value '%s': %s", opts.StorageClass, strings.Join(problems, ", "))
		}
	}
	if problems := validation.IsValidLabelValue(opts.DefaultPreference); len(problems) > 0 {
		return fmt.Errorf("invalid 'default_preference' value '%s': %s", opts.DefaultPreference, strings.Join(problems, ", "))
	}
	if problems := validation.IsValidLabelValue(opts.DefaultInstanceType); len(problems) > 0 {
		return fmt.Errorf("invalid 'default_instance_type' value '%s': %s", opts.DefaultInstanceType, strings.Join(problems, ", "))
	}
	return nil
}

func GenerateDataVolume(opts DataSourceOptions) *cdiv1beta1.DataVolume {
	var storageClassName *string
	if opts.StorageClass != "" {
		storageClassName = &opts.StorageClass
	}

	return &cdiv1beta1.DataVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.VolumeName,
			Namespace: opts.Namespace,
			Annotations: map[string]string{
				// no Virtual Machine uses the volume, the import would wait for one on some storage classes
				immediateBindingAnnotation: "true",
			},
		},
		Spec: cdiv1beta1.DataVolumeSpec{
			Source: &cdiv1beta1.DataVolumeSource{
				HTTP: &cdiv1beta1.DataVolumeSourceHTTP{
					URL:                opts.ExportServerUrl,
					CertConfigMap:      opts.VolumeName,
					SecretExtraHeaders: []string{opts.VolumeName},
				},
			},
			Storage: &cdiv1beta1.StorageSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				StorageClassName: storageClassName,
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(opts.VolumeSize),
					},
				},
			},
		},
	}
}

func GenerateDataVolumeSecret(dataVolume *cdiv1beta1.DataVolume, opts DataSourceOptions) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.VolumeName,
			Namespace: opts.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(dataVolume, cdiv1beta1.SchemeGroupVersion.WithKind(dataVolumeKind)),
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			exportTokenHeaderKey: fmt.Sprintf("%s:%s", steps.ExportTokenHeader, opts.ExportServerToken),
		},
	}
}

func GenerateDataVolumeConfigMap(dataVolume *cdiv1beta1.DataVolume, opts DataSourceOptions) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.VolumeName,
			Namespace: opts.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(dataVolume, cdiv1beta1.SchemeGroupVersion.WithKind(dataVolumeKind)),
			},
		},
		Data: map[string]string{
			exportServerCertificateKey: opts.ExportServerCertificate,
		},
	}
}

func GenerateDataSource(opts DataSourceOptions) *cdiv1beta1.DataSource {
	labels := map[string]string{}
	if opts.DefaultPreference != "" {
		labels[instancetypeapi.DefaultPreferenceLabel] = opts.DefaultPreference
	}
	if opts.DefaultInstanceType != "" {
		labels[instancetypeapi.DefaultInstancetypeLabel] = opts.DefaultInstanceType
	}

	return &cdiv1beta1.DataSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.Name,
			Namespace: opts.Namespace,
			Labels:    labels,
		},
		Spec: cdiv1beta1.DataSourceSpec{
			Source: cdiv1beta1.DataSourceSource{
				PVC: &cdiv1beta1.DataVolumeSourcePVC{
					Name:      opts.VolumeName,
					Namespace: opts.Namespace,
				},
			},
		},
	}
}

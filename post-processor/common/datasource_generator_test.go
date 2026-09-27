package common

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	instancetypeapi "kubevirt.io/api/instancetype"
	"strings"
	"testing"
	"time"
)

func newDataSourceOptions() DataSourceOptions {
	return DataSourceOptions{
		Name:                    "base-ubuntu",
		Namespace:               "images",
		VolumeName:              "base-ubuntu-20260926153045",
		ExportServerUrl:         "https://virt-export-base-ubuntu.packer.svc/volumes/base-ubuntu-source/disk.img.gz",
		ExportServerToken:       "export-token",
		ExportServerCertificate: "-----BEGIN CERTIFICATE-----",
		VolumeSize:              "10Gi",
		DefaultPreference:       "ubuntu",
	}
}

func TestBuildVolumeNameChangesWithBuildTime(t *testing.T) {
	buildTime := time.Date(2026, time.September, 26, 15, 30, 45, 0, time.UTC)

	name := BuildVolumeName("base-ubuntu", buildTime)
	if name != "base-ubuntu-20260926153045" {
		t.Errorf("expected volume name 'base-ubuntu-20260926153045', got: '%s'", name)
	}
	if nextName := BuildVolumeName("base-ubuntu", buildTime.Add(time.Second)); nextName == name {
		t.Errorf("expected a different volume name for a later build, got: '%s'", nextName)
	}
}

func TestGenerateDataVolumeImportsFromExportServer(t *testing.T) {
	opts := newDataSourceOptions()

	dataVolume := GenerateDataVolume(opts)

	if dataVolume.Name != opts.VolumeName || dataVolume.Namespace != opts.Namespace {
		t.Errorf("expected Data Volume '%s/%s', got: '%s/%s'", opts.Namespace, opts.VolumeName, dataVolume.Namespace, dataVolume.Name)
	}
	if _, found := dataVolume.Annotations["cdi.kubevirt.io/storage.bind.immediate.requested"]; !found {
		t.Errorf("expected the import to start without a Virtual Machine using the volume, got annotations: %v", dataVolume.Annotations)
	}
	source := dataVolume.Spec.Source.HTTP
	if source.URL != opts.ExportServerUrl {
		t.Errorf("expected source URL '%s', got: '%s'", opts.ExportServerUrl, source.URL)
	}

	configMap := GenerateDataVolumeConfigMap(dataVolume, opts)
	if source.CertConfigMap != configMap.Name {
		t.Errorf("expected certificate config map '%s', got: '%s'", configMap.Name, source.CertConfigMap)
	}
	secret := GenerateDataVolumeSecret(dataVolume, opts)
	if len(source.SecretExtraHeaders) != 1 || source.SecretExtraHeaders[0] != secret.Name {
		t.Errorf("expected header secret '%s', got: %v", secret.Name, source.SecretExtraHeaders)
	}
}

func TestGenerateDataVolumeStorage(t *testing.T) {
	opts := newDataSourceOptions()

	storage := GenerateDataVolume(opts).Spec.Storage
	if size := storage.Resources.Requests[corev1.ResourceStorage]; size.String() != "10Gi" {
		t.Errorf("expected volume size '10Gi', got: '%s'", size.String())
	}
	if storage.StorageClassName != nil {
		t.Errorf("expected the default storage class, got: '%s'", *storage.StorageClassName)
	}

	opts.StorageClass = "fast"
	storage = GenerateDataVolume(opts).Spec.Storage
	if storage.StorageClassName == nil || *storage.StorageClassName != "fast" {
		t.Errorf("expected storage class 'fast', got: %v", storage.StorageClassName)
	}
}

func TestGenerateDataVolumeSecretHoldsExportTokenHeader(t *testing.T) {
	opts := newDataSourceOptions()
	dataVolume := GenerateDataVolume(opts)
	dataVolume.UID = "data-volume-uid"

	secret := GenerateDataVolumeSecret(dataVolume, opts)

	if secret.Namespace != opts.Namespace {
		t.Errorf("expected secret namespace '%s', got: '%s'", opts.Namespace, secret.Namespace)
	}
	if len(secret.StringData) != 1 {
		t.Fatalf("expected one header in the secret, got keys: %d", len(secret.StringData))
	}
	for _, header := range secret.StringData {
		if header != "x-kubevirt-export-token:export-token" {
			t.Errorf("expected the export token header, got: '%s'", header)
		}
	}
	if !metav1.IsControlledBy(secret, dataVolume) {
		t.Errorf("expected the secret to be owned by the Data Volume, got: %v", secret.OwnerReferences)
	}
}

func TestGenerateDataVolumeConfigMapHoldsExportServerCertificate(t *testing.T) {
	opts := newDataSourceOptions()
	dataVolume := GenerateDataVolume(opts)
	dataVolume.UID = "data-volume-uid"

	configMap := GenerateDataVolumeConfigMap(dataVolume, opts)

	if configMap.Namespace != opts.Namespace {
		t.Errorf("expected config map namespace '%s', got: '%s'", opts.Namespace, configMap.Namespace)
	}
	if certificate := configMap.Data["tls.crt"]; certificate != opts.ExportServerCertificate {
		t.Errorf("expected the export server certificate under 'tls.crt', got: %v", configMap.Data)
	}
	if !metav1.IsControlledBy(configMap, dataVolume) {
		t.Errorf("expected the config map to be owned by the Data Volume, got: %v", configMap.OwnerReferences)
	}
}

func TestGenerateDataSourcePointsToVolume(t *testing.T) {
	opts := newDataSourceOptions()

	dataSource := GenerateDataSource(opts)

	if dataSource.Name != opts.Name || dataSource.Namespace != opts.Namespace {
		t.Errorf("expected Data Source '%s/%s', got: '%s/%s'", opts.Namespace, opts.Name, dataSource.Namespace, dataSource.Name)
	}
	volume := dataSource.Spec.Source.PVC
	if volume == nil || volume.Name != opts.VolumeName || volume.Namespace != opts.Namespace {
		t.Errorf("expected the Data Source to point to volume '%s/%s', got: %v", opts.Namespace, opts.VolumeName, volume)
	}
}

func TestGenerateDataSourceLabels(t *testing.T) {
	opts := newDataSourceOptions()

	labels := GenerateDataSource(opts).Labels
	if labels[instancetypeapi.DefaultPreferenceLabel] != "ubuntu" {
		t.Errorf("expected default preference 'ubuntu', got labels: %v", labels)
	}
	if _, found := labels[instancetypeapi.DefaultInstancetypeLabel]; found {
		t.Errorf("expected no default instance type, got labels: %v", labels)
	}

	opts.DefaultInstanceType = "u1.medium"
	labels = GenerateDataSource(opts).Labels
	if labels[instancetypeapi.DefaultInstancetypeLabel] != "u1.medium" {
		t.Errorf("expected default instance type 'u1.medium', got labels: %v", labels)
	}
}

func TestValidateDataSourceOptions(t *testing.T) {
	if err := ValidateDataSourceOptions(DataSourceOptions{}); err != nil {
		t.Errorf("expected options left empty to be valid, got: %v", err)
	}
	valid := newDataSourceOptions()
	valid.StorageClass = "fast"
	valid.DefaultPreference = "windows.10.virtio"
	valid.DefaultInstanceType = "u1.medium"
	if err := ValidateDataSourceOptions(valid); err != nil {
		t.Errorf("expected options to be valid, got: %v", err)
	}

	invalid := map[string]DataSourceOptions{
		"datasource_name":       {Name: "Base_Ubuntu"},
		"namespace":             {Namespace: "images.linux"},
		"volume_size":           {VolumeSize: "10 gigabytes"},
		"storage_class":         {StorageClass: "Fast Storage"},
		"default_preference":    {DefaultPreference: "ubuntu/26.04"},
		"default_instance_type": {DefaultInstanceType: strings.Repeat("u", 64)},
	}
	for field, opts := range invalid {
		err := ValidateDataSourceOptions(opts)
		if err == nil || !strings.Contains(err.Error(), "'"+field+"'") {
			t.Errorf("expected an error naming '%s', got: %v", field, err)
		}
	}
}

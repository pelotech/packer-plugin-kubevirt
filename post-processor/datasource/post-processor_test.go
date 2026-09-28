package datasource

import (
	"context"
	"errors"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	exportv1 "kubevirt.io/api/export/v1"
	instancetypeapi "kubevirt.io/api/instancetype"
	cdifake "kubevirt.io/client-go/containerizeddataimporter/fake"
	kubevirtfake "kubevirt.io/client-go/kubevirt/fake"
	cdiv1beta1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
	"os"
	buildercommon "packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// the CDI fake does not serve the streaming lists that client-go asks for by default
	_ = os.Setenv("KUBE_FEATURE_WatchListClient", "false")
	os.Exit(m.Run())
}

const exportServerUrl = "https://virt-export-base-ubuntu.packer.svc/volumes/base-ubuntu-source/disk.img.gz"

func newExport() *exportv1.VirtualMachineExport {
	return &exportv1.VirtualMachineExport{
		ObjectMeta: metav1.ObjectMeta{Name: "base-ubuntu", Namespace: "packer"},
		Status: &exportv1.VirtualMachineExportStatus{
			Links: &exportv1.VirtualMachineExportLinks{
				Internal: &exportv1.VirtualMachineExportLink{
					Cert: "-----BEGIN CERTIFICATE-----",
					Volumes: []exportv1.VirtualMachineExportVolume{
						{
							Name: "base-ubuntu-source",
							Formats: []exportv1.VirtualMachineExportVolumeFormat{
								{Format: exportv1.KubeVirtRaw, Url: "https://virt-export-base-ubuntu.packer.svc/volumes/base-ubuntu-source/disk.img"},
								{Format: exportv1.KubeVirtGz, Url: exportServerUrl},
							},
						},
					},
				},
			},
		},
	}
}

func newArtifact() packersdk.Artifact {
	return &buildercommon.KubevirtArtifact{
		StateData: map[string]interface{}{
			buildercommon.NamespaceArtifactKey:                 "packer",
			buildercommon.VirtualMachineExportNameArtifactKey:  "base-ubuntu",
			buildercommon.VirtualMachineExportTokenArtifactKey: "export-token",
			buildercommon.PreferenceArtifactKey:                "ubuntu",
			buildercommon.DiskSizeArtifactKey:                  "10Gi",
		},
	}
}

// newPostProcessor returns a post-processor whose import ends in the given phase
func newPostProcessor(config Config, importPhase cdiv1beta1.DataVolumePhase, export *exportv1.VirtualMachineExport, dataSources ...*cdiv1beta1.DataSource) *PostProcessor {
	cdiClient := cdifake.NewSimpleClientset()
	for _, dataSource := range dataSources {
		_ = cdiClient.Tracker().Add(dataSource)
	}
	watcher := watch.NewFakeWithChanSize(1, false)
	cdiClient.PrependWatchReactor("datavolumes", k8stesting.DefaultWatchReactor(watcher, nil))
	watcher.Modify(&cdiv1beta1.DataVolume{Status: cdiv1beta1.DataVolumeStatus{Phase: importPhase}})

	config.ImportTimeOut = 5 * time.Second
	return &PostProcessor{
		config: config,
		clients: &k8s.Clients{
			Kubernetes: k8sfake.NewSimpleClientset(),
			Kubevirt:   kubevirtfake.NewSimpleClientset(export),
			CDI:        cdiClient,
		},
	}
}

func findDataVolume(t *testing.T, p *PostProcessor, namespace string) *cdiv1beta1.DataVolume {
	dataVolumes, err := p.clients.CDI.CdiV1beta1().DataVolumes(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil || len(dataVolumes.Items) != 1 {
		t.Fatalf("expected one Data Volume in namespace '%s', got: %v, error: %v", namespace, dataVolumes.Items, err)
	}
	return &dataVolumes.Items[0]
}

func findDataSource(t *testing.T, p *PostProcessor, namespace, name string) *cdiv1beta1.DataSource {
	dataSource, err := p.clients.CDI.CdiV1beta1().DataSources(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected Data Source '%s/%s', got: %v", namespace, name, err)
	}
	return dataSource
}

func TestPostProcessUsesBuilderValues(t *testing.T) {
	p := newPostProcessor(Config{}, cdiv1beta1.Succeeded, newExport())

	_, _, _, err := p.PostProcess(context.Background(), packersdk.TestUi(t), newArtifact())
	if err != nil {
		t.Fatalf("expected the post-processor to succeed, got: %v", err)
	}

	dataVolume := findDataVolume(t, p, "packer")
	if !strings.HasPrefix(dataVolume.Name, "base-ubuntu-") {
		t.Errorf("expected a volume named after the Virtual Machine Export, got: '%s'", dataVolume.Name)
	}
	if url := dataVolume.Spec.Source.HTTP.URL; url != exportServerUrl {
		t.Errorf("expected the compressed image URL '%s', got: '%s'", exportServerUrl, url)
	}
	if size := dataVolume.Spec.Storage.Resources.Requests[corev1.ResourceStorage]; size.String() != "10Gi" {
		t.Errorf("expected the disk size of the builder '10Gi', got: '%s'", size.String())
	}

	dataSource := findDataSource(t, p, "packer", "base-ubuntu")
	if volume := dataSource.Spec.Source.PVC; volume == nil || volume.Name != dataVolume.Name || volume.Namespace != "packer" {
		t.Errorf("expected the Data Source to point to volume 'packer/%s', got: %v", dataVolume.Name, volume)
	}
	if preference := dataSource.Labels[instancetypeapi.DefaultPreferenceLabel]; preference != "ubuntu" {
		t.Errorf("expected the preference of the builder 'ubuntu', got labels: %v", dataSource.Labels)
	}
}

func TestPostProcessOverridesBuilderValues(t *testing.T) {
	config := Config{
		DataSourceName:      "ubuntu-26-04",
		DataSourceNamespace: "images",
		VolumeSize:          "20Gi",
		VolumeStorageClass:  "fast",
		DefaultPreference:   "ubuntu.26.04",
		DefaultInstanceType: "u1.medium",
	}
	p := newPostProcessor(config, cdiv1beta1.Succeeded, newExport())

	_, _, _, err := p.PostProcess(context.Background(), packersdk.TestUi(t), newArtifact())
	if err != nil {
		t.Fatalf("expected the post-processor to succeed, got: %v", err)
	}

	dataVolume := findDataVolume(t, p, "images")
	if !strings.HasPrefix(dataVolume.Name, "ubuntu-26-04-") {
		t.Errorf("expected a volume named after the Data Source, got: '%s'", dataVolume.Name)
	}
	if size := dataVolume.Spec.Storage.Resources.Requests[corev1.ResourceStorage]; size.String() != "20Gi" {
		t.Errorf("expected volume size '20Gi', got: '%s'", size.String())
	}
	if storageClass := dataVolume.Spec.Storage.StorageClassName; storageClass == nil || *storageClass != "fast" {
		t.Errorf("expected storage class 'fast', got: %v", storageClass)
	}

	labels := findDataSource(t, p, "images", "ubuntu-26-04").Labels
	if labels[instancetypeapi.DefaultPreferenceLabel] != "ubuntu.26.04" || labels[instancetypeapi.DefaultInstancetypeLabel] != "u1.medium" {
		t.Errorf("expected the configured preference and instance type, got labels: %v", labels)
	}
}

func TestPostProcessUpdatesExistingDataSource(t *testing.T) {
	existing := &cdiv1beta1.DataSource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "base-ubuntu",
			Namespace: "packer",
			Labels: map[string]string{
				"team":                                   "platform",
				instancetypeapi.DefaultPreferenceLabel:   "fedora",
				instancetypeapi.DefaultInstancetypeLabel: "u1.large",
			},
		},
		Spec: cdiv1beta1.DataSourceSpec{
			Source: cdiv1beta1.DataSourceSource{
				PVC: &cdiv1beta1.DataVolumeSourcePVC{Name: "base-ubuntu-20250101000000", Namespace: "packer"},
			},
		},
	}
	p := newPostProcessor(Config{}, cdiv1beta1.Succeeded, newExport(), existing)

	_, _, _, err := p.PostProcess(context.Background(), packersdk.TestUi(t), newArtifact())
	if err != nil {
		t.Fatalf("expected the post-processor to succeed, got: %v", err)
	}

	dataVolume := findDataVolume(t, p, "packer")
	dataSource := findDataSource(t, p, "packer", "base-ubuntu")
	if volume := dataSource.Spec.Source.PVC; volume.Name != dataVolume.Name || volume.Name == existing.Spec.Source.PVC.Name {
		t.Errorf("expected the Data Source to point to the new volume '%s', got: '%s'", dataVolume.Name, volume.Name)
	}
	if dataSource.Labels["team"] != "platform" || dataSource.Labels[instancetypeapi.DefaultPreferenceLabel] != "ubuntu" {
		t.Errorf("expected the preference to change and the other labels to stay, got labels: %v", dataSource.Labels)
	}
	if _, found := dataSource.Labels[instancetypeapi.DefaultInstancetypeLabel]; found {
		t.Errorf("expected the instance type left out of the configuration to be removed, got labels: %v", dataSource.Labels)
	}
}

func TestPostProcessRemovesImportResources(t *testing.T) {
	p := newPostProcessor(Config{}, cdiv1beta1.Succeeded, newExport())

	_, _, _, err := p.PostProcess(context.Background(), packersdk.TestUi(t), newArtifact())
	if err != nil {
		t.Fatalf("expected the post-processor to succeed, got: %v", err)
	}

	secrets, _ := p.clients.Kubernetes.CoreV1().Secrets("packer").List(context.Background(), metav1.ListOptions{})
	configMaps, _ := p.clients.Kubernetes.CoreV1().ConfigMaps("packer").List(context.Background(), metav1.ListOptions{})
	if len(secrets.Items) != 0 || len(configMaps.Items) != 0 {
		t.Errorf("expected the export token and certificate to be removed, got secrets: %v, config maps: %v", secrets.Items, configMaps.Items)
	}
	_, err = p.clients.Kubevirt.ExportV1().VirtualMachineExports("packer").Get(context.Background(), "base-ubuntu", metav1.GetOptions{})
	if !k8serrors.IsNotFound(err) {
		t.Errorf("expected the Virtual Machine Export to be deleted, got: %v", err)
	}
}

func TestPostProcessKeepsExport(t *testing.T) {
	p := newPostProcessor(Config{KeepExport: true}, cdiv1beta1.Succeeded, newExport())

	_, _, _, err := p.PostProcess(context.Background(), packersdk.TestUi(t), newArtifact())
	if err != nil {
		t.Fatalf("expected the post-processor to succeed, got: %v", err)
	}

	_, err = p.clients.Kubevirt.ExportV1().VirtualMachineExports("packer").Get(context.Background(), "base-ubuntu", metav1.GetOptions{})
	if err != nil {
		t.Errorf("expected the Virtual Machine Export to be kept, got: %v", err)
	}
}

func TestPostProcessFailedImportLeavesDataSourceUntouched(t *testing.T) {
	p := newPostProcessor(Config{}, cdiv1beta1.Failed, newExport())

	_, _, _, err := p.PostProcess(context.Background(), packersdk.TestUi(t), newArtifact())
	if err == nil {
		t.Fatal("expected the post-processor to fail")
	}

	dataSources, _ := p.clients.CDI.CdiV1beta1().DataSources("packer").List(context.Background(), metav1.ListOptions{})
	if len(dataSources.Items) != 0 {
		t.Errorf("expected no Data Source, got: %v", dataSources.Items)
	}
	dataVolumes, _ := p.clients.CDI.CdiV1beta1().DataVolumes("packer").List(context.Background(), metav1.ListOptions{})
	if len(dataVolumes.Items) != 0 {
		t.Errorf("expected the volume of the failed import to be deleted, got: %v", dataVolumes.Items)
	}
	_, err = p.clients.Kubevirt.ExportV1().VirtualMachineExports("packer").Get(context.Background(), "base-ubuntu", metav1.GetOptions{})
	if !k8serrors.IsNotFound(err) {
		t.Errorf("expected the Virtual Machine Export to be deleted, got: %v", err)
	}
}

func TestPostProcessStopsWaitingWhenCancelled(t *testing.T) {
	p := newPostProcessor(Config{}, cdiv1beta1.ImportInProgress, newExport())
	p.config.ImportTimeOut = 10 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(100*time.Millisecond, cancel)

	started := time.Now()
	_, _, _, err := p.PostProcess(ctx, packersdk.TestUi(t), newArtifact())
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("expected the post-processor to stop once cancelled, waited: %s", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected an error saying the build was cancelled, got: %v", err)
	}
}

func TestPostProcessWithoutDiskSize(t *testing.T) {
	p := newPostProcessor(Config{}, cdiv1beta1.Succeeded, newExport())
	artifact := &buildercommon.KubevirtArtifact{
		StateData: map[string]interface{}{
			buildercommon.NamespaceArtifactKey:                 "packer",
			buildercommon.VirtualMachineExportNameArtifactKey:  "base-ubuntu",
			buildercommon.VirtualMachineExportTokenArtifactKey: "export-token",
		},
	}

	_, _, _, err := p.PostProcess(context.Background(), packersdk.TestUi(t), artifact)
	if err == nil || !strings.Contains(err.Error(), "'volume_size'") {
		t.Fatalf("expected an error asking for 'volume_size', got: %v", err)
	}

	dataVolumes, _ := p.clients.CDI.CdiV1beta1().DataVolumes("packer").List(context.Background(), metav1.ListOptions{})
	if len(dataVolumes.Items) != 0 {
		t.Errorf("expected no Data Volume, got: %v", dataVolumes.Items)
	}
}

func TestConfigureRejectsInvalidValues(t *testing.T) {
	// keeps the test away from any cluster if the validation comes to pass
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "kubeconfig"))

	invalid := map[string]string{
		"datasource_name":       "Base_Ubuntu",
		"datasource_namespace":  "images.linux",
		"volume_size":           "10 gigabytes",
		"volume_storage_class":  "Fast Storage",
		"default_preference":    "ubuntu/26.04",
		"default_instance_type": "u1/medium",
	}
	for field, value := range invalid {
		err := new(PostProcessor).Configure(map[string]interface{}{field: value})
		if err == nil || !strings.Contains(err.Error(), "invalid '"+field+"' value '"+value+"'") {
			t.Errorf("expected an invalid '%s' error, got: %v", field, err)
		}
	}
}

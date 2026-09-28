//go:generate packer-sdc mapstructure-to-hcl2 -type Config

package datasource

import (
	"cmp"
	"context"
	"fmt"
	"github.com/hashicorp/hcl/v2/hcldec"
	packercommon "github.com/hashicorp/packer-plugin-sdk/common"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	instancetypeapi "kubevirt.io/api/instancetype"
	cdiv1beta1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
	"maps"
	buildercommon "packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/post-processor/common"
	"time"
)

type Config struct {
	packercommon.PackerConfig `mapstructure:",squash"`
	ctx                       interpolate.Context
	DataSourceName            string        `mapstructure:"datasource_name" required:"false"`
	DataSourceNamespace       string        `mapstructure:"datasource_namespace" required:"false"`
	VolumeSize                string        `mapstructure:"volume_size" required:"false"`
	VolumeStorageClass        string        `mapstructure:"volume_storage_class" required:"false"`
	DefaultPreference         string        `mapstructure:"default_preference" required:"false"`
	DefaultInstanceType       string        `mapstructure:"default_instance_type" required:"false"`
	ImportTimeOut             time.Duration `mapstructure:"import_timeout" required:"false"`
	KeepExport                bool          `mapstructure:"keep_export" required:"false"`
}

type PostProcessor struct {
	config  Config
	clients *k8s.Clients
}

func (p *PostProcessor) ConfigSpec() hcldec.ObjectSpec { return p.config.FlatMapstructure().HCL2Spec() }

func (p *PostProcessor) Configure(raws ...interface{}) error {
	err := config.Decode(&p.config, &config.DecodeOpts{
		PluginType:         "packer.post-processor.datasource",
		Interpolate:        true,
		InterpolateContext: &p.config.ctx,
		InterpolateFilter: &interpolate.RenderFilter{
			Exclude: []string{},
		},
	}, raws...)
	if err != nil {
		return err
	}

	err = common.ValidateDataSourceOptions(common.DataSourceOptions{
		Name:                p.config.DataSourceName,
		Namespace:           p.config.DataSourceNamespace,
		VolumeSize:          p.config.VolumeSize,
		StorageClass:        p.config.VolumeStorageClass,
		DefaultPreference:   p.config.DefaultPreference,
		DefaultInstanceType: p.config.DefaultInstanceType,
	})
	if err != nil {
		return err
	}

	if p.config.ImportTimeOut == 0 {
		p.config.ImportTimeOut = 10 * time.Minute
	}

	p.clients, err = k8s.GetKubevirtClient()
	if err != nil {
		return err
	}

	return nil
}

func (p *PostProcessor) PostProcess(ctx context.Context, ui packersdk.Ui, source packersdk.Artifact) (packersdk.Artifact, bool, bool, error) {
	preference, _ := source.State(buildercommon.PreferenceArtifactKey).(string)
	diskSize, _ := source.State(buildercommon.DiskSizeArtifactKey).(string)

	export, token, err := common.GetExport(p.clients, source)
	if err != nil {
		return nil, false, false, err
	}
	defer common.DeleteOrKeepExport(p.clients, ui, export.Namespace, export.Name, p.config.KeepExport)

	exportServerUrl := common.FindVolumeUrl(export, "")
	if exportServerUrl == "" {
		return nil, true, true, fmt.Errorf("failed to get the desired volume URL from Virtual Machine Export %s/%s: %v", export.Namespace, export.Name, export.Status)
	}

	dataSourceName := cmp.Or(p.config.DataSourceName, export.Name)
	options := common.DataSourceOptions{
		Name:                    dataSourceName,
		Namespace:               cmp.Or(p.config.DataSourceNamespace, export.Namespace),
		VolumeName:              common.BuildVolumeName(dataSourceName, time.Now()),
		ExportServerUrl:         exportServerUrl,
		ExportServerToken:       token,
		ExportServerCertificate: export.Status.Links.Internal.Cert,
		VolumeSize:              cmp.Or(p.config.VolumeSize, diskSize),
		StorageClass:            p.config.VolumeStorageClass,
		DefaultPreference:       cmp.Or(p.config.DefaultPreference, preference),
		DefaultInstanceType:     p.config.DefaultInstanceType,
	}
	if options.VolumeSize == "" {
		return nil, true, true, fmt.Errorf("the builder gave no disk size, 'volume_size' must be set")
	}
	err = common.ValidateDataSourceOptions(options)
	if err != nil {
		return nil, true, true, err
	}

	err = p.importVolume(ctx, ui, options)
	if err != nil {
		return nil, true, true, err
	}

	err = p.applyDataSource(options)
	if err != nil {
		return nil, true, true, fmt.Errorf("failed to apply Data Source %s/%s: %w", options.Namespace, options.Name, err)
	}
	ui.Message(fmt.Sprintf("Data Source %s/%s points to volume %s", options.Namespace, options.Name, options.VolumeName))

	return source, true, true, nil
}

func (p *PostProcessor) importVolume(ctx context.Context, ui packersdk.Ui, options common.DataSourceOptions) error {
	dataVolumes := p.clients.CDI.CdiV1beta1().DataVolumes(options.Namespace)
	dataVolume, err := dataVolumes.Create(ctx, common.GenerateDataVolume(options), metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create Data Volume: %w", err)
	}

	err = p.waitForImport(ctx, ui, dataVolume, options)
	if err != nil {
		deleteErr := dataVolumes.Delete(context.TODO(), dataVolume.Name, metav1.DeleteOptions{})
		if deleteErr != nil {
			ui.Error(fmt.Sprintf("failed to delete Data Volume %s/%s: %v", dataVolume.Namespace, dataVolume.Name, deleteErr))
		}
		return err
	}

	return nil
}

func (p *PostProcessor) waitForImport(ctx context.Context, ui packersdk.Ui, dataVolume *cdiv1beta1.DataVolume, options common.DataSourceOptions) error {
	secrets := p.clients.Kubernetes.CoreV1().Secrets(options.Namespace)
	secret, err := secrets.Create(ctx, common.GenerateDataVolumeSecret(dataVolume, options), metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create Data Volume secret: %w", err)
	}
	defer func() { _ = secrets.Delete(context.TODO(), secret.Name, metav1.DeleteOptions{}) }()

	configMaps := p.clients.Kubernetes.CoreV1().ConfigMaps(options.Namespace)
	configMap, err := configMaps.Create(ctx, common.GenerateDataVolumeConfigMap(dataVolume, options), metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create Data Volume config map: %w", err)
	}
	defer func() { _ = configMaps.Delete(context.TODO(), configMap.Name, metav1.DeleteOptions{}) }()

	err = k8s.WaitForDataVolumeImport(ctx, p.clients, ui, dataVolume, p.config.ImportTimeOut)
	if err != nil {
		return fmt.Errorf("error with Data Volume %s/%s: %w", dataVolume.Namespace, dataVolume.Name, err)
	}

	return nil
}

func (p *PostProcessor) applyDataSource(options common.DataSourceOptions) error {
	dataSources := p.clients.CDI.CdiV1beta1().DataSources(options.Namespace)
	dataSource := common.GenerateDataSource(options)

	existing, err := dataSources.Get(context.TODO(), dataSource.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = dataSources.Create(context.TODO(), dataSource, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}

	existing.Spec.Source = dataSource.Spec.Source
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	delete(existing.Labels, instancetypeapi.DefaultPreferenceLabel)
	delete(existing.Labels, instancetypeapi.DefaultInstancetypeLabel)
	maps.Copy(existing.Labels, dataSource.Labels)
	_, err = dataSources.Update(context.TODO(), existing, metav1.UpdateOptions{})
	return err
}

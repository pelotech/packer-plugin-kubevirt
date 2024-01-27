//go:generate packer-sdc mapstructure-to-hcl2 -type Config

package s3

import (
	"context"
	"fmt"
	"github.com/hashicorp/hcl/v2/hcldec"
	packercommon "github.com/hashicorp/packer-plugin-sdk/common"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kubevirt.io/api/export/v1alpha1"
	"kubevirt.io/client-go/kubecli"
	buildercommon "packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	vmgenerator "packer-plugin-kubevirt/builder/common/k8s/resourcegenerator"
	"packer-plugin-kubevirt/post-processor/common"
)

type Config struct {
	packercommon.PackerConfig `mapstructure:",squash"`
	ctx                       interpolate.Context
	S3Bucket                  string `mapstructure:"s3_bucket"`
	S3Key                     string `mapstructure:"s3_key"`
	AWSAccessKeyId            string `mapstructure:"aws_access_key_id"`
	AWSSecretAccessKey        string `mapstructure:"aws_secret_access_key"`
	AWSRegion                 string `mapstructure:"aws_region"`
}

type PostProcessor struct {
	config     Config
	virtClient kubecli.KubevirtClient
}

func (p *PostProcessor) ConfigSpec() hcldec.ObjectSpec { return p.config.FlatMapstructure().HCL2Spec() }

func (p *PostProcessor) Configure(raws ...interface{}) error {
	err := config.Decode(&p.config, &config.DecodeOpts{
		PluginType:         "packer.post-processor.s3",
		Interpolate:        true,
		InterpolateContext: &p.config.ctx,
		InterpolateFilter: &interpolate.RenderFilter{
			Exclude: []string{},
		},
	}, raws...)
	if err != nil {
		return err
	}

	p.virtClient, err = k8s.GetInClusterKubevirtClient()
	if err != nil {
		return err
	}

	return nil
}

func (p *PostProcessor) PostProcess(_ context.Context, ui packersdk.Ui, source packersdk.Artifact) (packersdk.Artifact, bool, bool, error) {
	export := source.State(string(buildercommon.VirtualMachineExport)).(*v1alpha1.VirtualMachineExport)
	exportToken := source.State(string(buildercommon.VirtualMachineExportToken)).(string)

	defer func() {
		err := p.cleanupResources(export.Namespace, export.Name)
		if err != nil {
			ui.Error(fmt.Sprintf("Failed to clean up VM export: %v", err))
		}
	}()

	var exportServerUrl string
	for _, vol := range export.Status.Links.Internal.Volumes {
		if vol.Name == string(vmgenerator.PrimaryVolumeDiskMapping) {
			for _, volumeFormat := range vol.Formats {
				if volumeFormat.Format == v1alpha1.KubeVirtGz || volumeFormat.Format == v1alpha1.ArchiveGz {
					exportServerUrl = volumeFormat.Url
				}
			}
		}
	}
	if exportServerUrl == "" {
		return nil, true, true, fmt.Errorf("failed to find export server url")
	}

	options := common.S3UploaderOptions{
		Name:               export.Name,
		Namespace:          export.Namespace,
		ExportServerUrl:    exportServerUrl,
		ExportServerToken:  exportToken,
		S3BucketName:       p.config.S3Bucket,
		S3Key:              p.config.S3Key,
		AWSAccessKeyId:     p.config.AWSAccessKeyId,
		AWSSecretAccessKey: p.config.AWSSecretAccessKey,
		AWSRegion:          p.config.AWSRegion,
	}

	job := common.GenerateS3UploaderJob(export, options)
	job, err := p.virtClient.BatchV1().Jobs(export.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		return nil, true, true, fmt.Errorf("failed to deploy S3 uploader job: %w", err)
	}

	secret := common.GenerateS3UploaderSecret(job, options)
	_, err = p.virtClient.CoreV1().Secrets(export.Namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	if err != nil {
		return nil, true, true, fmt.Errorf("failed to create S3 uploader secret: %w", err)
	}

	return source, true, true, nil
}

func (p *PostProcessor) cleanupResources(ns, name string) error {
	return p.virtClient.VirtualMachineExport(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

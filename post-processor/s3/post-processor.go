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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"kubevirt.io/api/export/v1alpha1"
	"kubevirt.io/client-go/kubecli"
	buildercommon "packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	vmgenerator "packer-plugin-kubevirt/builder/common/k8s/generator"
	"packer-plugin-kubevirt/post-processor/common"
	"strings"
	"time"
)

type Config struct {
	packercommon.PackerConfig `mapstructure:",squash"`
	ctx                       interpolate.Context
	S3Bucket                  string        `mapstructure:"s3_bucket"`
	S3KeyPrefix               string        `mapstructure:"s3_key_prefix"`
	AWSAccessKeyId            string        `mapstructure:"aws_access_key_id"`
	AWSSecretAccessKey        string        `mapstructure:"aws_secret_access_key"`
	AWSRegion                 string        `mapstructure:"aws_region"`
	UploadTimeOut             time.Duration `mapstructure:"upload_timeout" required:"false"`
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

	p.virtClient, err = k8s.GetKubevirtClient()
	if err != nil {
		return err
	}

	if p.config.UploadTimeOut == 0 {
		p.config.UploadTimeOut = 10 * time.Minute
	}

	return nil
}

func (p *PostProcessor) PostProcess(_ context.Context, ui packersdk.Ui, source packersdk.Artifact) (packersdk.Artifact, bool, bool, error) {
	ns := source.State(buildercommon.NamespaceArtifactKey).(string)
	name := source.State(buildercommon.VirtualMachineExportNameArtifactKey).(string)
	token := source.State(buildercommon.VirtualMachineExportTokenArtifactKey).(string)

	export, err := p.virtClient.VirtualMachineExport(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, false, false, fmt.Errorf("failed to get Virtual Machine Export: %w", err)
	}
	defer p.cleanupResources(ui, ns, name)

	var exportServerUrl string
	for _, vol := range export.Status.Links.Internal.Volumes {
		if strings.HasSuffix(vol.Name, string(vmgenerator.SourceDataVolumeSuffix)) { // may need better logic if many volumes
			for _, volumeFormat := range vol.Formats {
				if volumeFormat.Format == v1alpha1.KubeVirtGz {
					exportServerUrl = volumeFormat.Url
				}
			}
		}
	}
	if exportServerUrl == "" {
		return nil, true, true, fmt.Errorf("failed to get export server url from Virtual Machine Export %s/%s: %v", ns, name, export.Status)
	}

	options := common.S3UploaderOptions{
		Name:               export.Name,
		Namespace:          export.Namespace,
		ExportServerUrl:    exportServerUrl,
		ExportServerToken:  token,
		S3BucketName:       p.config.S3Bucket,
		S3KeyPrefix:        p.config.S3KeyPrefix,
		AWSAccessKeyId:     p.config.AWSAccessKeyId,
		AWSSecretAccessKey: p.config.AWSSecretAccessKey,
		AWSRegion:          p.config.AWSRegion,
	}

	job := common.GenerateS3UploaderJob(export, options)
	job, err = p.virtClient.BatchV1().Jobs(export.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		return nil, true, true, fmt.Errorf("failed to deploy S3 uploader job: %w", err)
	}

	secret := common.GenerateS3UploaderSecret(job, options)
	_, err = p.virtClient.CoreV1().Secrets(export.Namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	if err != nil {
		return nil, true, true, fmt.Errorf("failed to create S3 uploader secret: %w", err)
	}

	err = p.waitForJobCompletion(ui, job, p.config.UploadTimeOut)
	if err != nil {
		return nil, true, true, fmt.Errorf("failed to get S3 uploader job successfully completed: %w", err)
	}

	return source, true, true, nil
}

func (p *PostProcessor) waitForJobCompletion(ui packersdk.Ui, job *batchv1.Job, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	watcher, err := p.virtClient.BatchV1().Jobs(job.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: labels.SelectorFromSet(map[string]string{
			"metadata.name": job.Name,
		}).String(),
	})
	if err != nil {
		return fmt.Errorf("failed to get S3 Uploader Job state %s/%s: %w", job.Namespace, job.Name, err)
	}

	for {
		select {
		case event, _ := <-watcher.ResultChan():
			updatedJob, _ := event.Object.(*batchv1.Job)
			for index, condition := range updatedJob.Status.Conditions {
				if index == 0 {
					ui.Message(fmt.Sprintf("condition '%s' changed to '%s'", condition.Type, condition.Status))
				}
				if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
					return nil
				} else if (condition.Type == batchv1.JobFailed || condition.Type == batchv1.JobFailureTarget) && condition.Status == corev1.ConditionTrue {
					return fmt.Errorf("failed to upload export with S3 Uploader Job")
				}
			}

		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for S3 Uploader Job to be completed")
		}
	}
}

func (p *PostProcessor) cleanupResources(ui packersdk.Ui, ns, name string) {
	err := p.virtClient.VirtualMachineExport(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err == nil {
		ui.Message(fmt.Sprintf("Virtual Machine Export %s/%s has been deleted", ns, name))
	} else {
		ui.Error(fmt.Sprintf("failed to delete Virtual Machine Export  %s/%s: %v", ns, name, err))
	}
}

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
	"net/url"
	buildercommon "packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/post-processor/common"
	"regexp"
	"time"
)

type Config struct {
	packercommon.PackerConfig `mapstructure:",squash"`
	ctx                       interpolate.Context
	S3Bucket                  string `mapstructure:"s3_bucket"`
	S3KeyPrefix               string `mapstructure:"s3_key_prefix"`
	S3ObjectName              string `mapstructure:"s3_object_name" required:"false"`
	S3EndpointUrl             string `mapstructure:"s3_endpoint_url" required:"false"`

	ServiceAccountName string        `mapstructure:"service_account_name"`
	AWSAccessKeyId     string        `mapstructure:"aws_access_key_id"`
	AWSSecretAccessKey string        `mapstructure:"aws_secret_access_key"`
	AWSRegion          string        `mapstructure:"aws_region"`
	UploadTimeOut      time.Duration `mapstructure:"upload_timeout" required:"false"`
	ImageFormat        string        `mapstructure:"image_format" required:"false"`
	KeepExport         bool          `mapstructure:"keep_export" required:"false"`
}

type PostProcessor struct {
	config  Config
	clients *k8s.Clients
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

	err = common.ValidateImageFormat(p.config.ImageFormat)
	if err != nil {
		return err
	}

	err = validateEndpointUrl(p.config.S3EndpointUrl)
	if err != nil {
		return err
	}

	err = validateObjectName(p.config.S3ObjectName)
	if err != nil {
		return err
	}

	p.clients, err = k8s.GetKubevirtClient()
	if err != nil {
		return err
	}

	if p.config.UploadTimeOut == 0 {
		p.config.UploadTimeOut = 10 * time.Minute
	}

	if (p.config.AWSAccessKeyId == "" || p.config.AWSSecretAccessKey == "") && p.config.ServiceAccountName == "" {
		return fmt.Errorf("either AWS access keys or service account name must be provided")
	}

	return nil
}

func validateEndpointUrl(endpointUrl string) error {
	if endpointUrl == "" {
		return nil
	}
	parsed, err := url.Parse(endpointUrl)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("invalid 's3_endpoint_url' value '%s': expected an http or https URL", endpointUrl)
	}
	return nil
}

// the name ends up in the command line of the upload
var objectNamePattern = regexp.MustCompile(`^[A-Za-z0-9._+-]*$`)

func validateObjectName(objectName string) error {
	if !objectNamePattern.MatchString(objectName) {
		return fmt.Errorf("invalid 's3_object_name' value '%s': expected letters, digits, '.', '_', '+' or '-'", objectName)
	}
	return nil
}

func (p *PostProcessor) PostProcess(_ context.Context, ui packersdk.Ui, source packersdk.Artifact) (packersdk.Artifact, bool, bool, error) {
	ns := source.State(buildercommon.NamespaceArtifactKey).(string)
	name := source.State(buildercommon.VirtualMachineExportNameArtifactKey).(string)
	token := source.State(buildercommon.VirtualMachineExportTokenArtifactKey).(string)

	export, err := p.clients.Kubevirt.ExportV1().VirtualMachineExports(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, false, false, fmt.Errorf("failed to get Virtual Machine Export: %w", err)
	}
	defer common.DeleteOrKeepExport(p.clients, ui, ns, name, p.config.KeepExport)

	exportServerUrl := common.FindVolumeUrl(export, p.config.ImageFormat)
	if exportServerUrl == "" {
		return nil, true, true, fmt.Errorf("failed to get the desired volume URL from Virtual Machine Export %s/%s: %v", ns, name, export.Status)
	}

	options := common.S3UploaderOptions{
		Name:                    export.Name,
		Namespace:               export.Namespace,
		ExportServerUrl:         exportServerUrl,
		ExportServerToken:       token,
		ExportServerCertificate: export.Status.Links.Internal.Cert,
		S3BucketName:            p.config.S3Bucket,
		S3KeyPrefix:             p.config.S3KeyPrefix,
		ObjectName:              p.config.S3ObjectName,
		S3EndpointUrl:           p.config.S3EndpointUrl,
		AWSRegion:               p.config.AWSRegion,
		ImageFormat:             p.config.ImageFormat,
	}
	if p.config.ServiceAccountName != "" {
		// Priority to IRSA-based auth
		options.ServiceAccountName = &p.config.ServiceAccountName
	} else {
		// Default to AWS credentials
		options.AWSAccessKeyId = &p.config.AWSAccessKeyId
		options.AWSSecretAccessKey = &p.config.AWSSecretAccessKey
	}

	job := common.GenerateS3UploaderJob(export, options)
	job, err = p.clients.Kubernetes.BatchV1().Jobs(export.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		return nil, true, true, fmt.Errorf("failed to deploy S3 uploader job: %w", err)
	}

	secret := common.GenerateS3UploaderSecret(job, options)
	_, err = p.clients.Kubernetes.CoreV1().Secrets(export.Namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	if err != nil {
		return nil, true, true, fmt.Errorf("failed to create S3 uploader secret: %w", err)
	}

	err = k8s.WaitForJobCompletion(p.clients.Kubernetes, ui, job, p.config.UploadTimeOut)
	if err != nil {
		return nil, true, true, fmt.Errorf("error with 'S3 uploader' job: %w", err)
	}

	return source, true, true, nil
}

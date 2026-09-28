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
	"net/url"
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
	S3AccessKeyId      string        `mapstructure:"s3_access_key_id"`
	S3SecretAccessKey  string        `mapstructure:"s3_secret_access_key"`
	S3Region           string        `mapstructure:"s3_region"`
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

	if (p.config.S3AccessKeyId == "" || p.config.S3SecretAccessKey == "") && p.config.ServiceAccountName == "" {
		return fmt.Errorf("either 's3_access_key_id' and 's3_secret_access_key' or 'service_account_name' must be set")
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

func (p *PostProcessor) PostProcess(ctx context.Context, ui packersdk.Ui, source packersdk.Artifact) (packersdk.Artifact, bool, bool, error) {
	export, token, err := common.GetExport(p.clients, source)
	if err != nil {
		return nil, false, false, err
	}
	defer common.DeleteOrKeepExport(p.clients, ui, export.Namespace, export.Name, p.config.KeepExport)

	exportServerUrl := common.FindVolumeUrl(export, p.config.ImageFormat)
	if exportServerUrl == "" {
		return nil, true, true, fmt.Errorf("failed to get the desired volume URL from Virtual Machine Export %s/%s: %v", export.Namespace, export.Name, export.Status)
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
		AWSRegion:               p.config.S3Region,
		ImageFormat:             p.config.ImageFormat,
	}
	if p.config.ServiceAccountName != "" {
		// Priority to IRSA-based auth
		options.ServiceAccountName = p.config.ServiceAccountName
	} else {
		// Default to AWS credentials
		options.AWSAccessKeyId = p.config.S3AccessKeyId
		options.AWSSecretAccessKey = p.config.S3SecretAccessKey
	}

	generateSecret := func(job *batchv1.Job) *corev1.Secret { return common.GenerateS3UploaderSecret(job, options) }
	err = common.RunUploadJob(ctx, p.clients, ui, "S3 uploader", common.GenerateS3UploaderJob(export, options), generateSecret, p.config.UploadTimeOut)
	if err != nil {
		return nil, true, true, err
	}

	return source, true, true, nil
}

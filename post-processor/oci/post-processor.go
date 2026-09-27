//go:generate packer-sdc mapstructure-to-hcl2 -type Config

package oci

import (
	"context"
	"fmt"
	"github.com/hashicorp/hcl/v2/hcldec"
	packercommon "github.com/hashicorp/packer-plugin-sdk/common"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	buildercommon "packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/post-processor/common"
	"slices"
	"strings"
	"time"
)

var supportedImageFormats = []string{"qcow2", "raw"}

type Config struct {
	packercommon.PackerConfig `mapstructure:",squash"`
	ctx                       interpolate.Context
	Image                     string `mapstructure:"image"`

	ServiceAccountName  string        `mapstructure:"service_account_name" required:"false"`
	RegistryUsername    string        `mapstructure:"registry_username" required:"false"`
	RegistryPassword    string        `mapstructure:"registry_password" required:"false"`
	RegistrySecretName  string        `mapstructure:"registry_secret_name" required:"false"`
	RegistryInsecure    bool          `mapstructure:"registry_insecure" required:"false"`
	UploadTimeOut       time.Duration `mapstructure:"upload_timeout" required:"false"`
	KeepExport          bool          `mapstructure:"keep_export" required:"false"`
	ImageFormat         string        `mapstructure:"image_format" required:"false"`
	DefaultPreference   string        `mapstructure:"default_preference" required:"false"`
	DefaultInstanceType string        `mapstructure:"default_instance_type" required:"false"`
}

type PostProcessor struct {
	config  Config
	clients *k8s.Clients
}

func (p *PostProcessor) ConfigSpec() hcldec.ObjectSpec { return p.config.FlatMapstructure().HCL2Spec() }

func (p *PostProcessor) Configure(raws ...interface{}) error {
	err := config.Decode(&p.config, &config.DecodeOpts{
		PluginType:         "packer.post-processor.oci",
		Interpolate:        true,
		InterpolateContext: &p.config.ctx,
		InterpolateFilter: &interpolate.RenderFilter{
			Exclude: []string{},
		},
	}, raws...)
	if err != nil {
		return err
	}

	if p.config.ImageFormat == "" {
		p.config.ImageFormat = "qcow2"
	}

	if p.config.UploadTimeOut == 0 {
		p.config.UploadTimeOut = 10 * time.Minute
	}

	err = p.config.validate()
	if err != nil {
		return err
	}

	p.clients, err = k8s.GetKubevirtClient()
	if err != nil {
		return err
	}

	return nil
}

func (c *Config) validate() error {
	if c.Image == "" {
		return fmt.Errorf("'image' is required")
	}

	// a digest is only known once the image is pushed
	name := c.Image[strings.LastIndex(c.Image, "/")+1:]
	if !strings.Contains(name, ":") || strings.Contains(name, "@") {
		return fmt.Errorf("'image' must carry a tag and no digest, got: '%s'", c.Image)
	}

	if !slices.Contains(supportedImageFormats, c.ImageFormat) {
		return fmt.Errorf("unsupported image format '%s', allowed values: %s", c.ImageFormat, strings.Join(supportedImageFormats, ", "))
	}

	if (c.RegistryUsername == "") != (c.RegistryPassword == "") {
		return fmt.Errorf("'registry_username' and 'registry_password' must be provided together")
	}

	if c.RegistrySecretName != "" && c.RegistryUsername != "" {
		return fmt.Errorf("'registry_secret_name' cannot be used with 'registry_username' and 'registry_password'")
	}

	return nil
}

func (p *PostProcessor) findDefaultPreference(source packersdk.Artifact) string {
	if p.config.DefaultPreference != "" {
		return p.config.DefaultPreference
	}

	preference, _ := source.State(buildercommon.PreferenceArtifactKey).(string)
	return preference
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

	options := common.OCIUploaderOptions{
		Name:                    export.Name,
		Namespace:               export.Namespace,
		ServiceAccountName:      p.config.ServiceAccountName,
		ExportServerUrl:         exportServerUrl,
		ExportServerToken:       token,
		ExportServerCertificate: export.Status.Links.Internal.Cert,
		Image:                   p.config.Image,
		ImageFormat:             p.config.ImageFormat,
		RegistryUsername:        p.config.RegistryUsername,
		RegistryPassword:        p.config.RegistryPassword,
		RegistrySecretName:      p.config.RegistrySecretName,
		RegistryInsecure:        p.config.RegistryInsecure,
		DefaultPreference:       p.findDefaultPreference(source),
		DefaultInstanceType:     p.config.DefaultInstanceType,
	}

	job := common.GenerateOCIUploaderJob(export, options)
	job, err = p.clients.Kubernetes.BatchV1().Jobs(export.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		return nil, true, true, fmt.Errorf("failed to deploy OCI uploader job: %w", err)
	}

	secret := common.GenerateOCIUploaderSecret(job, options)
	_, err = p.clients.Kubernetes.CoreV1().Secrets(export.Namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	if err != nil {
		return nil, true, true, fmt.Errorf("failed to create OCI uploader secret: %w", err)
	}

	err = k8s.WaitForJobCompletion(p.clients.Kubernetes, ui, job, p.config.UploadTimeOut)
	if err != nil {
		return nil, true, true, fmt.Errorf("error with 'OCI uploader' job: %w", err)
	}

	return source, true, true, nil
}

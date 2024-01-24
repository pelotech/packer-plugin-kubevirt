//go:generate packer-sdc mapstructure-to-hcl2 -type Config

package s3

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/common"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
	"os"
	awsutils "packer-plugin-kubevirt/post-processor/s3/aws"
)

type Config struct {
	common.PackerConfig `mapstructure:",squash"`
	MockOption          string `mapstructure:"mock"`
	ctx                 interpolate.Context
}

type PostProcessor struct {
	config Config
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

	return nil
}

func (p *PostProcessor) PostProcess(_ context.Context, ui packersdk.Ui, source packersdk.Artifact) (packersdk.Artifact, bool, bool, error) {
	ui.Say(fmt.Sprintf("post-processor mock: %s", p.config.MockOption))

	// TODO: Filepath needs to be added to Artifact object + S3 bucket/key passed
	file, err := os.Open("")
	defer file.Close()
	if err != nil {
		return nil, true, true, fmt.Errorf("failed to open VM image file: %w", err)
	}

	err = uploadImage(file, "", "")
	if err != nil {
		return nil, true, true, fmt.Errorf("failed to upload VM image: %w", err)
	}

	return source, true, true, nil
}

func uploadImage(file *os.File, bucket, key string) error {
	uploader, err := awsutils.GetS3Uploader()

	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   file,
	})
	if err != nil {
		return err
	}

	return nil
}

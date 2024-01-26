//go:generate packer-sdc mapstructure-to-hcl2 -type Config

package iso

import (
	"context"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	gossh "golang.org/x/crypto/ssh"
	"kubevirt.io/client-go/kubecli"
	buildercommon "packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	vmgenerator "packer-plugin-kubevirt/builder/common/k8s/vm-generator"
	stepDef "packer-plugin-kubevirt/builder/common/steps"
	"packer-plugin-kubevirt/builder/common/utils"
)

const (
	BuilderId              = "kubevirt.iso"
	VirtualMachineHost     = "127.0.0.1"
	VirtualMachineUsername = "packer"
	VirtualMachinePassword = "packer"
	DefaultSSHPort         = 22
	DefaultWinRMPort       = 5985
)

type Config struct {
	common.PackerConfig `mapstructure:",squash"`

	KubernetesName      string `mapstructure:"kubernetes_name"`
	KubernetesNamespace string `mapstructure:"kubernetes_namespace"`

	KubevirtOsPreference string `mapstructure:"kubevirt_os_preference"`

	VirtualMachineDiskSpace string `mapstructure:"vm_disk_space"`

	SSHPort   int `mapstructure:"ssh_port"`
	WinRMPort int `mapstructure:"winrm_port"`

	SourceS3Url              string `mapstructure:"source_s3_url"`
	SourceAWSAccessKeyId     string `mapstructure:"source_aws_access_key_id"`
	SourceAWSSecretAccessKey string `mapstructure:"source_aws_secret_access_key"`
}

type Builder struct {
	config     Config
	runner     multistep.Runner
	virtClient kubecli.KubevirtClient
}

func (b *Builder) ConfigSpec() hcldec.ObjectSpec {
	return b.config.FlatMapstructure().HCL2Spec()
}

func (b *Builder) Prepare(raws ...interface{}) (generatedVars []string, warnings []string, err error) {
	err = config.Decode(&b.config, &config.DecodeOpts{
		PluginType:  BuilderId,
		Interpolate: true,
	}, raws...)
	if err != nil {
		return nil, nil, err
	}

	// 0. Align logger log level on user bool input 'b.config.PackerDebug'	INFO/DEBUG
	// 1. Validate configuration fields
	// 2. Validate credentials (e.g. kubectl client)
	// 3. Any computed values (if needed)

	b.virtClient, err = k8s.GetKubevirtClient()
	if err != nil {
		return nil, nil, err
	}

	return []string{}, nil, nil
}

func (b *Builder) Run(ctx context.Context, ui packer.Ui, hook packer.Hook) (packer.Artifact, error) {

	steps := []multistep.Step{
		&stepDef.StepDeployVM{
			VirtClient: b.virtClient,
			VmOptions: vmgenerator.VirtualMachineOptions{
				Name:         b.config.KubernetesName,
				Namespace:    b.config.KubernetesNamespace,
				OsPreference: b.config.KubevirtOsPreference,
				S3ImageSource: vmgenerator.S3ImageSource{
					URL:                b.config.SourceS3Url,
					AWSAccessKeyId:     b.config.SourceAWSAccessKeyId,
					AWSSecretAccessKey: b.config.SourceAWSSecretAccessKey,
				},
				DiskSpace: b.config.VirtualMachineDiskSpace,
			},
		},
		&stepDef.StepPortForwardVM{
			VirtClient: b.virtClient,
		},
		&communicator.StepConnect{
			Host: func(bag multistep.StateBag) (string, error) {
				return VirtualMachineHost, nil
			},
			SSHConfig: func(bag multistep.StateBag) (*gossh.ClientConfig, error) {
				return &gossh.ClientConfig{
					User: VirtualMachineUsername,
					Auth: []gossh.AuthMethod{
						gossh.Password(VirtualMachinePassword),
					},
					HostKeyCallback: gossh.InsecureIgnoreHostKey(),
				}, nil
			},
			SSHPort: func(bag multistep.StateBag) (int, error) {
				return utils.GetOrDefault(b.config.SSHPort, DefaultSSHPort), nil
			},
			WinRMConfig: func(bag multistep.StateBag) (*communicator.WinRMConfig, error) {
				return &communicator.WinRMConfig{
					Username: VirtualMachineUsername,
					Password: VirtualMachinePassword,
				}, nil
			},
			WinRMPort: func(bag multistep.StateBag) (int, error) {
				return utils.GetOrDefault(b.config.WinRMPort, DefaultWinRMPort), nil
			},
		},
		&commonsteps.StepProvision{},
		&stepDef.StepExportVM{
			VirtClient: b.virtClient,
		},
	}

	state := new(multistep.BasicStateBag)
	appContext := &buildercommon.AppContext{State: state}
	appContext.Put(buildercommon.PackerHook, hook)
	appContext.Put(buildercommon.PackerUi, ui)

	// Run!
	b.runner = commonsteps.NewRunner(steps, b.config.PackerConfig, ui)
	b.runner.Run(ctx, state)

	// If there was an error, return that
	err := appContext.GetPackerError()
	if err != nil {
		return nil, err
	}

	return appContext.BuildArtifact(), nil
}

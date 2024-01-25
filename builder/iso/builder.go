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
	"packer-plugin-kubevirt/builder/common/k8s"
	vmgenerator "packer-plugin-kubevirt/builder/common/k8s/vm-generator"
	stepDef "packer-plugin-kubevirt/builder/common/steps"
	"packer-plugin-kubevirt/builder/common/utils"
)

const (
	BuilderId              = "kubevirt.iso"
	VirtualMachineUsername = "packer"
	VirtualMachinePassword = "packer"
)

type Config struct {
	common.PackerConfig `mapstructure:",squash"`

	KubernetesName      string `mapstructure:"kubernetes_name"`
	KubernetesNamespace string `mapstructure:"kubernetes_namespace"`

	KubevirtOsPreference string `mapstructure:"kubevirt_os_preference"`

	VirtualMachineDiskSpace string `mapstructure:"vm_disk_space"`

	SSHPort   int `mapstructure:"ssh_port"`
	WinRMPort int `mapstructure:"winrm_port"`

	SourceS3Url             string `mapstructure:"source_s3_url"`
	SourceS3AccessKeyId     string `mapstructure:"source_s3_access_key_id"`
	SourceS3SecretAccessKey string `mapstructure:"source_s3_secret_access_key"`
}

type Builder struct {
	config Config
	runner multistep.Runner
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

	return []string{}, nil, nil
}

func (b *Builder) Run(ctx context.Context, ui packer.Ui, hook packer.Hook) (packer.Artifact, error) {
	virtClient, _ := k8s.GetKubevirtClient()

	steps := []multistep.Step{
		&stepDef.StepDeployVM{
			VirtClient: virtClient,
			VmOptions: vmgenerator.VirtualMachineOptions{
				Name:         b.config.KubernetesName,
				Namespace:    b.config.KubernetesNamespace,
				OsPreference: b.config.KubevirtOsPreference,
				S3ImageSource: vmgenerator.S3ImageSource{
					Url:                b.config.SourceS3Url,
					AwsAccessKeyId:     b.config.SourceS3AccessKeyId,
					AwsSecretAccessKey: b.config.SourceS3SecretAccessKey,
				},
				DiskSpace: b.config.VirtualMachineDiskSpace,
			},
		},
		&stepDef.StepPortForwardVM{
			VirtClient: virtClient,
		},
		&communicator.StepConnect{
			Host: func(bag multistep.StateBag) (string, error) {
				return "127.0.0.1", nil
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
				return utils.GetOrDefault(b.config.WinRMPort, 22), nil
			},
			WinRMConfig: func(bag multistep.StateBag) (*communicator.WinRMConfig, error) {
				return &communicator.WinRMConfig{
					Username: VirtualMachineUsername,
					Password: VirtualMachinePassword,
				}, nil
			},
			WinRMPort: func(bag multistep.StateBag) (int, error) {
				return utils.GetOrDefault(b.config.WinRMPort, 5985), nil
			},
		},
		// This step pulls the communicator config from 'communicator' key in state bag (populated by StepConnect)
		&commonsteps.StepProvision{},
		&stepDef.StepExportVM{
			VirtClient: virtClient,
		},
		&stepDef.StepPortForwardVM{
			VirtClient: virtClient,
		},
		&stepDef.StepDownloadVM{
			VirtClient: virtClient,
		},
	}

	state := new(multistep.BasicStateBag)
	state.Put("hook", hook)
	state.Put("ui", ui)

	state.Put("namespace", b.config.KubernetesNamespace)
	state.Put("name", b.config.KubernetesName)

	// Run!
	b.runner = commonsteps.NewRunner(steps, b.config.PackerConfig, ui)
	b.runner.Run(ctx, state)

	// If there was an error, return that
	if err, ok := state.GetOk("error"); ok {
		return nil, err.(error)
	}

	artifact := &Artifact{
		// Add the builder generated data to the artifact StateData so that post-processors
		// can access them.
		StateData: map[string]interface{}{
			"vm_export_filepath": state.Get(),
		},
	}

	return artifact, nil
}

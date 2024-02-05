//go:generate packer-sdc mapstructure-to-hcl2 -type Config

package iso

import (
	"context"
	"fmt"
	awsv1beta1 "github.com/aws/karpenter/pkg/apis/v1beta1"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	gossh "golang.org/x/crypto/ssh"
	"k8s.io/apimachinery/pkg/runtime"
	"kubevirt.io/client-go/kubecli"
	buildercommon "packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/k8s/generator"
	stepDef "packer-plugin-kubevirt/builder/common/steps"
	"packer-plugin-kubevirt/builder/common/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	v1beta1 "sigs.k8s.io/karpenter/pkg/apis/v1beta1"
	"time"
)

const (
	builderId = "kubevirt.iso"
)

type Config struct {
	common.PackerConfig             `mapstructure:",squash"`
	Comm                            communicator.Config `mapstructure:",squash"`
	KubernetesName                  string              `mapstructure:"kubernetes_name"`
	KubernetesNamespace             string              `mapstructure:"kubernetes_namespace"`
	KubernetesNodeAutoscaler        k8s.NodeAutoscaler  `mapstructure:"kubernetes_node_autoscaler" required:"false"`
	KubevirtOsPreference            string              `mapstructure:"kubevirt_os_preference"`
	VirtualMachineDiskSpace         string              `mapstructure:"vm_disk_space"`
	VirtualMachineDeploymentTimeOut time.Duration       `mapstructure:"vm_deployment_timeout" required:"false"`
	VirtualMachineExportTimeOut     time.Duration       `mapstructure:"vm_export_timeout" required:"false"`
	SourceUrl                       string              `mapstructure:"source_url"`
	SourceAWSAccessKeyId            string              `mapstructure:"source_aws_access_key_id" required:"false"`
	SourceAWSSecretAccessKey        string              `mapstructure:"source_aws_secret_access_key" required:"false"`
}

type Builder struct {
	config     Config
	runner     multistep.Runner
	virtClient kubecli.KubevirtClient
	kubeClient client.Client
}

func (b *Builder) ConfigSpec() hcldec.ObjectSpec {
	return b.config.FlatMapstructure().HCL2Spec()
}

func (b *Builder) Prepare(raws ...interface{}) (generatedVars []string, warnings []string, err error) {
	err = config.Decode(&b.config, &config.DecodeOpts{
		PluginType:  builderId,
		Interpolate: true,
	}, raws...)
	if err != nil {
		return nil, nil, err
	}

	// 1. Align logger log level on user bool input 'b.config.PackerDebug'	INFO/DEBUG
	// 2. Validate configuration fields
	// 3. Validate credentials (e.g. k8s clients)
	// 4. Any computed values (if needed)
	if utils.IsReservedPort(b.config.Comm.SSHPort) || utils.IsReservedPort(b.config.Comm.WinRMPort) {
		return nil, nil, fmt.Errorf("the local port for communicating with the remote machine is reserved - please use a port above 1024")
	}

	if b.config.KubernetesNodeAutoscaler == "" {
		b.config.KubernetesNodeAutoscaler = k8s.DefaultNodeAutoscaler
	}

	if b.config.VirtualMachineDeploymentTimeOut == 0 {
		b.config.VirtualMachineDeploymentTimeOut = 10 * time.Minute
	}

	if b.config.VirtualMachineExportTimeOut == 0 {
		b.config.VirtualMachineExportTimeOut = 5 * time.Minute
	}

	if b.config.Comm.Type == "" {
		b.config.Comm.Type = "ssh"
		warnings = append(warnings, "no communication method was specified, so SSH will be used by default to connect to the machine.")
	}

	b.virtClient, err = k8s.GetKubevirtClient()
	if err != nil {
		return nil, nil, err
	}

	scheme := runtime.NewScheme()
	builders := []runtime.SchemeBuilder{
		awsv1beta1.SchemeBuilder,
		v1beta1.SchemeBuilder,
	}
	for _, builder := range builders {
		err = builder.AddToScheme(scheme)
		if err != nil {
			return nil, nil, err
		}
	}
	b.kubeClient, err = client.New(b.virtClient.Config(), client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, nil, err
	}

	return generatedVars, warnings, nil
}

func (b *Builder) Run(ctx context.Context, ui packer.Ui, hook packer.Hook) (packer.Artifact, error) {

	steps := []multistep.Step{
		&stepDef.StepDeployVM{
			VirtClient:               b.virtClient,
			KubeClient:               b.kubeClient,
			KubernetesNodeAutoscaler: b.config.KubernetesNodeAutoscaler,
			VmOptions: generator.VirtualMachineOptions{
				Name:         b.config.KubernetesName,
				Namespace:    b.config.KubernetesNamespace,
				OsPreference: b.config.KubevirtOsPreference,
				ImageSource: generator.ImageSource{
					URL:                b.config.SourceUrl,
					AWSAccessKeyId:     b.config.SourceAWSAccessKeyId,
					AWSSecretAccessKey: b.config.SourceAWSSecretAccessKey,
				},
				DiskSpace: b.config.VirtualMachineDiskSpace,
			},
			VmDeploymentTimeOut: b.config.VirtualMachineDeploymentTimeOut,
		},
		&stepDef.StepPortForwardVM{
			VirtClient: b.virtClient,
			PortMappings: []string{
				fmt.Sprintf("%d:%d", utils.GetOrDefault(b.config.Comm.SSHPort, buildercommon.DefaultSSHPort), buildercommon.DefaultSSHPort),
				fmt.Sprintf("%d:%d", utils.GetOrDefault(b.config.Comm.WinRMPort, buildercommon.DefaultWinRMPort), buildercommon.DefaultWinRMPort),
			},
		},
		&communicator.StepConnect{
			Config: &b.config.Comm,
			Host: func(bag multistep.StateBag) (string, error) {
				return buildercommon.VirtualMachineHost, nil
			},
			SSHConfig: func(bag multistep.StateBag) (*gossh.ClientConfig, error) {
				return &gossh.ClientConfig{
					User: buildercommon.VirtualMachineUsername,
					Auth: []gossh.AuthMethod{
						gossh.Password(buildercommon.VirtualMachinePassword),
					},
					HostKeyCallback: gossh.InsecureIgnoreHostKey(),
				}, nil
			},
			SSHPort: func(bag multistep.StateBag) (int, error) {
				return utils.GetOrDefault(b.config.Comm.SSHPort, buildercommon.DefaultSSHPort), nil
			},
			WinRMConfig: func(bag multistep.StateBag) (*communicator.WinRMConfig, error) {
				return &communicator.WinRMConfig{
					Username: buildercommon.VirtualMachineUsername,
					Password: buildercommon.VirtualMachinePassword,
				}, nil
			},
			WinRMPort: func(bag multistep.StateBag) (int, error) {
				return utils.GetOrDefault(b.config.Comm.WinRMPort, buildercommon.DefaultWinRMPort), nil
			},
		},
		&commonsteps.StepProvision{},
		&stepDef.StepExportVM{
			VirtClient:      b.virtClient,
			VmExportTimeOut: b.config.VirtualMachineExportTimeOut,
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

	return appContext.BuildArtifact(builderId), nil
}

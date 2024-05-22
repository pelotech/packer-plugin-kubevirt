//go:generate packer-sdc mapstructure-to-hcl2 -type Config

package iso

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	gossh "golang.org/x/crypto/ssh"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"kubevirt.io/client-go/kubecli"
	"log"
	buildercommon "packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/k8s/generator"
	stepDef "packer-plugin-kubevirt/builder/common/steps"
	"packer-plugin-kubevirt/builder/common/vm"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
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
	KubernetesNodeSelectors         map[string]string   `mapstructure:"kubernetes_node_selectors"`
	KubernetesTolerations           []map[string]string `mapstructure:"kubernetes_tolerations"`
	KubevirtOsPreference            string              `mapstructure:"kubevirt_os_preference"`
	SourceUrl                       string              `mapstructure:"source_url"`
	SourceAWSAccessKeyId            string              `mapstructure:"source_aws_access_key_id" required:"false"`
	SourceAWSSecretAccessKey        string              `mapstructure:"source_aws_secret_access_key" required:"false"`
	VirtualMachineDiskSpace         string              `mapstructure:"vm_disk_space"`
	VirtualMachineDeploymentTimeOut time.Duration       `mapstructure:"vm_deployment_timeout" required:"false"`
	VirtualMachineExportTimeOut     time.Duration       `mapstructure:"vm_export_timeout" required:"false"`
	VirtualMachineLinuxCloudInit    string              `mapstructure:"vm_linux_cloud_init" required:"false"`
	VirtualMachineWindowsSysprep    string              `mapstructure:"vm_windows_sysprep" required:"false"`
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

	// TODO: Align logger log level on user bool input 'b.config.PackerDebug'	INFO/DEBUG

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
	commType := strings.ToLower(b.config.Comm.Type)
	if commType == "ssh" && b.config.Comm.SSHPort == 0 {
		b.config.Comm.SSHPort = 2222
	}
	if commType == "winrm" && b.config.Comm.WinRMPort == 0 {
		b.config.Comm.WinRMPort = 5389
	}
	if buildercommon.IsReservedPort(b.config.Comm.SSHPort) || buildercommon.IsReservedPort(b.config.Comm.WinRMPort) {
		return nil, nil, fmt.Errorf("the local port for communicating with the remote machine is reserved - please use a port above 1024")
	}
	if b.config.Comm.WinRMTimeout == 0 {
		b.config.Comm.WinRMTimeout = 30 * time.Second
	}

	b.virtClient, err = k8s.GetKubevirtClient()
	if err != nil {
		return nil, nil, err
	}

	scheme := runtime.NewScheme()
	builders := []runtime.SchemeBuilder{
		// Add your `SchemeBuilder` containing CRDs (if needed)
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

func decodeTolerations(rawTolerations []map[string]string) []v1.Toleration {
	var tolerations []v1.Toleration
	for _, rawToleration := range rawTolerations {
		var toleration v1.Toleration
		serializedToleration, _ := json.Marshal(rawToleration)
		reader := strings.NewReader(string(serializedToleration))
		err := yaml.NewYAMLOrJSONDecoder(reader, 4096).Decode(&toleration)
		if err != nil {
			log.Printf("Error deserializing tolerations: %s", err)
		}
		tolerations = append(tolerations, toleration)
	}
	return tolerations
}

func (b *Builder) Run(ctx context.Context, ui packer.Ui, hook packer.Hook) (packer.Artifact, error) {
	state := new(multistep.BasicStateBag)
	appContext := &buildercommon.AppContext{State: state}
	appContext.Put(buildercommon.PackerHook, hook)
	appContext.Put(buildercommon.PackerUi, ui)

	osFamily := vm.GetOSFamily(b.config.KubevirtOsPreference)
	appContext.Put(buildercommon.VirtualMachineOsFamily, &osFamily)

	steps := []multistep.Step{
		&stepDef.StepDeployVM{
			VirtClient: b.virtClient,
			KubeClient: b.kubeClient,
			VmOptions: generator.VirtualMachineOptions{
				Name:           b.config.KubernetesName,
				Namespace:      b.config.KubernetesNamespace,
				NodeSelectors:  b.config.KubernetesNodeSelectors,
				Tolerations:    decodeTolerations(b.config.KubernetesTolerations),
				OsDistribution: b.config.KubevirtOsPreference,
				OsFamily:       osFamily,
				DiskSpace:      b.config.VirtualMachineDiskSpace,
				ImageSource: generator.ImageSource{
					URL:                b.config.SourceUrl,
					AWSAccessKeyId:     b.config.SourceAWSAccessKeyId,
					AWSSecretAccessKey: b.config.SourceAWSSecretAccessKey,
				},
				UserProvisioning: generator.UserProvisioning{
					CloudInit: b.config.VirtualMachineLinuxCloudInit,
					Sysprep:   b.config.VirtualMachineWindowsSysprep,
				},
			},
			VmDeploymentTimeOut: b.config.VirtualMachineDeploymentTimeOut,
		},
		&stepDef.StepPortForwardVM{
			VirtClient: b.virtClient,
			Comm:       b.config.Comm,
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
				return buildercommon.GetOrDefault(b.config.Comm.SSHPort, buildercommon.DefaultSSHPort), nil
			},
			WinRMConfig: func(bag multistep.StateBag) (*communicator.WinRMConfig, error) {
				return &communicator.WinRMConfig{
					Username: buildercommon.VirtualMachineUsername,
					Password: buildercommon.VirtualMachinePassword,
				}, nil
			},
			WinRMPort: func(bag multistep.StateBag) (int, error) {
				return buildercommon.GetOrDefault(b.config.Comm.WinRMPort, buildercommon.DefaultWinRMPort), nil
			},
		},
		&commonsteps.StepProvision{},
		&stepDef.StepExportVM{
			VirtClient:      b.virtClient,
			VmExportTimeOut: b.config.VirtualMachineExportTimeOut,
		},
	}

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

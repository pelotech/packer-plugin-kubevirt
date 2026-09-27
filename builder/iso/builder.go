//go:generate packer-sdc mapstructure-to-hcl2 -type Config

package iso

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/bootcommand"
	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/shutdowncommand"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"log"
	buildercommon "packer-plugin-kubevirt/builder/common"
	"packer-plugin-kubevirt/builder/common/k8s"
	"packer-plugin-kubevirt/builder/common/k8s/generator"
	stepDef "packer-plugin-kubevirt/builder/common/steps"
	"packer-plugin-kubevirt/builder/common/vm"
	"time"
)

const (
	builderId = "kubevirt.iso"
)

type Config struct {
	common.PackerConfig             `mapstructure:",squash"`
	Comm                            communicator.Config `mapstructure:",squash"`
	bootcommand.BootConfig          `mapstructure:",squash"`
	BootKeyInterval                 time.Duration `mapstructure:"boot_key_interval" required:"false"`
	shutdowncommand.ShutdownConfig  `mapstructure:",squash"`
	KubernetesName                  string              `mapstructure:"kubernetes_name"`
	KubernetesNamespace             string              `mapstructure:"kubernetes_namespace"`
	KubernetesNodeSelectors         map[string]string   `mapstructure:"kubernetes_node_selectors"`
	KubernetesTolerations           []map[string]string `mapstructure:"kubernetes_tolerations"`
	KubevirtOsPreference            string              `mapstructure:"kubevirt_os_preference"`
	SourceUrl                       string              `mapstructure:"source_url"`
	SourceAWSAccessKeyId            string              `mapstructure:"source_aws_access_key_id" required:"false"`
	SourceAWSSecretAccessKey        string              `mapstructure:"source_aws_secret_access_key" required:"false"`
	VirtualMachineDiskSpace         string              `mapstructure:"vm_disk_space"`
	VirtualMachineCPU               string              `mapstructure:"vm_cpu" required:"false"`
	VirtualMachineMemory            string              `mapstructure:"vm_memory" required:"false"`
	VirtualMachineDeploymentTimeOut time.Duration       `mapstructure:"vm_deployment_timeout" required:"false"`
	VirtualMachineExportTimeOut     time.Duration       `mapstructure:"vm_export_timeout" required:"false"`
	VirtualMachineLinuxCloudInit    string              `mapstructure:"vm_linux_cloud_init" required:"false"`
	VirtualMachineWindowsSysprep    string              `mapstructure:"vm_windows_sysprep" required:"false"`
}

type Builder struct {
	config  Config
	clients *k8s.Clients
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

	if b.config.VirtualMachineCPU == "" {
		b.config.VirtualMachineCPU = "4"
	}
	if _, err = resource.ParseQuantity(b.config.VirtualMachineCPU); err != nil {
		return nil, nil, fmt.Errorf("invalid 'vm_cpu' value '%s': %s", b.config.VirtualMachineCPU, err)
	}

	if b.config.VirtualMachineMemory == "" {
		b.config.VirtualMachineMemory = "8Gi"
	}
	if _, err = resource.ParseQuantity(b.config.VirtualMachineMemory); err != nil {
		return nil, nil, fmt.Errorf("invalid 'vm_memory' value '%s': %s", b.config.VirtualMachineMemory, err)
	}

	if errs := b.config.BootConfig.Prepare(&interpolate.Context{}); len(errs) > 0 {
		return nil, nil, &packer.MultiError{Errors: errs}
	}
	b.config.ShutdownConfig.Prepare(&interpolate.Context{})

	warnings, err = prepareCommunicator(&b.config.Comm)
	if err != nil {
		return nil, nil, err
	}

	b.clients, err = k8s.GetKubevirtClient()
	if err != nil {
		return nil, nil, err
	}

	return generatedVars, warnings, nil
}

func prepareCommunicator(comm *communicator.Config) (warnings []string, err error) {
	if comm.Type == "" {
		comm.Type = "ssh"
		warnings = append(warnings, "no communication method was specified, so SSH will be used by default to connect to the machine.")
	}
	switch comm.Type {
	case "ssh":
		if comm.SSHUsername == "" && comm.SSHPassword == "" {
			comm.SSHUsername = buildercommon.VirtualMachineUsername
			comm.SSHPassword = buildercommon.VirtualMachinePassword
		}
	case "winrm":
		if comm.WinRMUser == "" && comm.WinRMPassword == "" {
			comm.WinRMUser = buildercommon.VirtualMachineUsername
			comm.WinRMPassword = buildercommon.VirtualMachinePassword
		}
	default:
		return nil, fmt.Errorf("unsupported communicator type, allowed values: 'ssh', 'winrm'")
	}
	if buildercommon.IsReservedPort(comm.SSHPort) || buildercommon.IsReservedPort(comm.WinRMPort) {
		return nil, fmt.Errorf("the local port for communicating with the remote machine is reserved - please use a port above 1024")
	}
	if comm.WinRMTimeout == 0 {
		comm.WinRMTimeout = 30 * time.Second
	}
	localSSHPort, localWinRMPort := comm.SSHPort, comm.WinRMPort
	if errs := comm.Prepare(nil); len(errs) > 0 {
		return nil, &packer.MultiError{Errors: errs}
	}
	// Prepare sets the ports left unset to 22 and 5985, while a free local port is wanted
	comm.SSHPort, comm.WinRMPort = localSSHPort, localWinRMPort

	return warnings, nil
}

func connectStep(comm *communicator.Config) *communicator.StepConnect {
	return &communicator.StepConnect{
		Config: comm,
		Host: func(bag multistep.StateBag) (string, error) {
			return buildercommon.VirtualMachineHost, nil
		},
		SSHConfig: comm.SSHConfigFunc(),
	}
}

func decodeTolerations(rawTolerations []map[string]string) []v1.Toleration {
	var tolerations []v1.Toleration
	for _, rawToleration := range rawTolerations {
		var toleration v1.Toleration
		serializedToleration, _ := json.Marshal(rawToleration)
		err := json.Unmarshal(serializedToleration, &toleration)
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
	appContext.Put(buildercommon.Preference, b.config.KubevirtOsPreference)
	appContext.Put(buildercommon.DiskSize, b.config.VirtualMachineDiskSpace)

	steps := []multistep.Step{
		&stepDef.StepDeployVM{
			Clients: b.clients,
			VmOptions: generator.VirtualMachineOptions{
				Name:           b.config.KubernetesName,
				Namespace:      b.config.KubernetesNamespace,
				NodeSelectors:  b.config.KubernetesNodeSelectors,
				Tolerations:    decodeTolerations(b.config.KubernetesTolerations),
				OsDistribution: b.config.KubevirtOsPreference,
				OsFamily:       osFamily,
				DiskSpace:      b.config.VirtualMachineDiskSpace,
				CPU:            b.config.VirtualMachineCPU,
				Memory:         b.config.VirtualMachineMemory,
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
		},
		&stepDef.StepBootCommand{
			Clients:          b.clients,
			BootCommand:      b.config.BootCommand,
			BootWait:         b.config.BootWait,
			KeyInterval:      b.config.BootKeyInterval,
			KeyGroupInterval: b.config.BootGroupInterval,
			Timeout:          b.config.VirtualMachineDeploymentTimeOut,
		},
		&stepDef.StepWaitForVM{
			Clients:             b.clients,
			VmDeploymentTimeOut: b.config.VirtualMachineDeploymentTimeOut,
		},
		&stepDef.StepPortForwardVM{
			Clients: b.clients,
			Comm:    &b.config.Comm,
		},
		connectStep(&b.config.Comm),
		&commonsteps.StepProvision{},
		&stepDef.StepShutdownVM{
			Clients:         b.clients,
			ShutdownCommand: b.config.ShutdownCommand,
			ShutdownTimeout: b.config.ShutdownTimeout,
		},
		&stepDef.StepExportVM{
			Clients:         b.clients,
			VmExportTimeOut: b.config.VirtualMachineExportTimeOut,
		},
	}

	// Run!
	runner := commonsteps.NewRunner(steps, b.config.PackerConfig, ui)
	runner.Run(ctx, state)

	// If there was an error, return that
	err := appContext.GetPackerError()
	if err != nil {
		return nil, err
	}

	return appContext.BuildArtifact(builderId), nil
}

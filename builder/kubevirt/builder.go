//go:generate packer-sdc mapstructure-to-hcl2 -type Config

package kubevirt

import (
	"context"
	"encoding/json"
	"errors"
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
	builderId = "kubevirt"
)

type Config struct {
	common.PackerConfig            `mapstructure:",squash"`
	Comm                           communicator.Config `mapstructure:",squash"`
	bootcommand.BootConfig         `mapstructure:",squash"`
	BootKeyInterval                time.Duration `mapstructure:"boot_key_interval" required:"false"`
	shutdowncommand.ShutdownConfig `mapstructure:",squash"`
	KubernetesNamespace            string              `mapstructure:"kubernetes_namespace"`
	KubernetesNodeSelector         map[string]string   `mapstructure:"kubernetes_node_selector"`
	KubernetesTolerations          []map[string]string `mapstructure:"kubernetes_tolerations"`
	SourceUrl                      string              `mapstructure:"source_url"`
	SourceAWSAccessKeyId           string              `mapstructure:"source_aws_access_key_id" required:"false"`
	SourceAWSSecretAccessKey       string              `mapstructure:"source_aws_secret_access_key" required:"false"`
	VirtualMachineName             string              `mapstructure:"vm_name"`
	VirtualMachinePreference       string              `mapstructure:"vm_preference"`
	VirtualMachineDiskSize         string              `mapstructure:"vm_disk_size"`
	VirtualMachineCPU              string              `mapstructure:"vm_cpu" required:"false"`
	VirtualMachineMemory           string              `mapstructure:"vm_memory" required:"false"`
	VirtualMachineInstallTimeOut   time.Duration       `mapstructure:"vm_install_timeout" required:"false"`
	VirtualMachineExportTimeOut    time.Duration       `mapstructure:"vm_export_timeout" required:"false"`
	VirtualMachineExportTTL        time.Duration       `mapstructure:"vm_export_ttl" required:"false"`
	VirtualMachineSkipVirtSysprep  bool                `mapstructure:"vm_skip_virt_sysprep" required:"false"`
	VirtualMachineCloudInit        string              `mapstructure:"vm_cloud_init" required:"false"`
	VirtualMachineAutounattend     string              `mapstructure:"vm_autounattend" required:"false"`
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

	if b.config.VirtualMachineInstallTimeOut == 0 {
		b.config.VirtualMachineInstallTimeOut = 10 * time.Minute
	}

	if b.config.VirtualMachineExportTimeOut == 0 {
		b.config.VirtualMachineExportTimeOut = 5 * time.Minute
	}

	if b.config.VirtualMachineExportTTL < 0 {
		return nil, nil, fmt.Errorf("invalid 'vm_export_ttl' value '%s': must be positive", b.config.VirtualMachineExportTTL)
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

	osFamily := vm.GetOSFamily(b.config.VirtualMachinePreference)
	appContext.Put(buildercommon.VirtualMachineOsFamily, &osFamily)
	appContext.Put(buildercommon.Preference, b.config.VirtualMachinePreference)
	appContext.Put(buildercommon.DiskSize, b.config.VirtualMachineDiskSize)

	steps := []multistep.Step{
		&stepDef.StepDeployVM{
			Clients: b.clients,
			VmOptions: generator.VirtualMachineOptions{
				Name:         b.config.VirtualMachineName,
				Namespace:    b.config.KubernetesNamespace,
				NodeSelector: b.config.KubernetesNodeSelector,
				Tolerations:  decodeTolerations(b.config.KubernetesTolerations),
				Preference:   b.config.VirtualMachinePreference,
				OsFamily:     osFamily,
				DiskSize:     b.config.VirtualMachineDiskSize,
				CPU:          b.config.VirtualMachineCPU,
				Memory:       b.config.VirtualMachineMemory,
				ImageSource: generator.ImageSource{
					URL:                b.config.SourceUrl,
					AWSAccessKeyId:     b.config.SourceAWSAccessKeyId,
					AWSSecretAccessKey: b.config.SourceAWSSecretAccessKey,
				},
				UserProvisioning: generator.UserProvisioning{
					CloudInit:    b.config.VirtualMachineCloudInit,
					Autounattend: b.config.VirtualMachineAutounattend,
				},
			},
		},
		&stepDef.StepBootCommand{
			Clients:          b.clients,
			BootCommand:      b.config.BootCommand,
			BootWait:         b.config.BootWait,
			KeyInterval:      b.config.BootKeyInterval,
			KeyGroupInterval: b.config.BootGroupInterval,
			Timeout:          b.config.VirtualMachineInstallTimeOut,
		},
		&stepDef.StepWaitForVM{
			Clients:          b.clients,
			VmInstallTimeOut: b.config.VirtualMachineInstallTimeOut,
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
		&stepDef.StepGeneralize{
			Clients:         b.clients,
			SkipVirtSysprep: b.config.VirtualMachineSkipVirtSysprep,
			VmExportTimeOut: b.config.VirtualMachineExportTimeOut,
		},
		&stepDef.StepExportVM{
			Clients:         b.clients,
			VmExportTimeOut: b.config.VirtualMachineExportTimeOut,
			VmExportTTL:     b.config.VirtualMachineExportTTL,
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
	// a cancelled step halts without an error, and leaves no export to build an artifact from
	if _, cancelled := state.GetOk(multistep.StateCancelled); cancelled {
		return nil, errors.New("build was cancelled")
	}
	if _, halted := state.GetOk(multistep.StateHalted); halted {
		return nil, errors.New("build was halted")
	}

	return appContext.BuildArtifact(builderId), nil
}

package common

import (
	"errors"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	kubevirtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1"
)

type StateBagEntry string

const (
	PackerHook                StateBagEntry = "hook"
	PackerUi                  StateBagEntry = "ui"
	PackerError               StateBagEntry = "error"
	VirtualMachine            StateBagEntry = "vm"
	VirtualMachineExport      StateBagEntry = "vmexport"
	VirtualMachineExportToken StateBagEntry = "vmexporttoken"
	// the export owns the stopped Virtual Machine, its deletion removes the disk
	VirtualMachineOwnedByExport StateBagEntry = "vmownedbyexport"

	VirtualMachineHost     = "127.0.0.1"
	VirtualMachineUsername = "packer"
	VirtualMachinePassword = "packer"
	DefaultSSHPort         = 22
	DefaultWinRMPort       = 5985
)

type AppContext struct {
	State multistep.StateBag
}

func (s *AppContext) GetPackerError() error {
	err := s.get(PackerError)
	if err != nil {
		return err.(error)
	}
	return nil
}

func (s *AppContext) GetPackerUi() packersdk.Ui {
	return s.get(PackerUi).(packersdk.Ui)
}

func (s *AppContext) Halt(err error) multistep.StepAction {
	s.Put(PackerError, err)
	s.GetPackerUi().Error(err.Error())
	return multistep.ActionHalt
}

func (s *AppContext) GetVirtualMachine() *kubevirtv1.VirtualMachine {
	vm := s.get(VirtualMachine)
	if vm != nil {
		return vm.(*kubevirtv1.VirtualMachine)
	}
	return nil
}

func (s *AppContext) GetVirtualMachineExport() *exportv1.VirtualMachineExport {
	export := s.get(VirtualMachineExport)
	if export != nil {
		return export.(*exportv1.VirtualMachineExport)
	}
	return nil
}

func (s *AppContext) GetVirtualMachineExportToken() string {
	return s.get(VirtualMachineExportToken).(string)
}

func (s *AppContext) IsVirtualMachineOwnedByExport() bool {
	owned, _ := s.get(VirtualMachineOwnedByExport).(bool)
	return owned
}

// BuildError returns why the build stopped: the error of the step that halted it, or its cancellation
func (s *AppContext) BuildError() error {
	if err := s.GetPackerError(); err != nil {
		return err
	}
	// a cancelled step halts without an error
	if _, cancelled := s.State.GetOk(multistep.StateCancelled); cancelled {
		return errors.New("build was cancelled")
	}
	if _, halted := s.State.GetOk(multistep.StateHalted); halted {
		return errors.New("build was halted")
	}
	return nil
}

// BuildFailed tells whether a step halted the build or it was cancelled
func (s *AppContext) BuildFailed() bool {
	return s.BuildError() != nil
}

func (s *AppContext) BuildArtifact(builderId, preference, diskSize string) packersdk.Artifact {
	return &KubevirtArtifact{
		BuilderIdValue: builderId,
		StateData: map[string]interface{}{
			NamespaceArtifactKey:                 s.GetVirtualMachineExport().Namespace,
			VirtualMachineExportNameArtifactKey:  s.GetVirtualMachineExport().Name,
			VirtualMachineExportTokenArtifactKey: s.GetVirtualMachineExportToken(),
			PreferenceArtifactKey:                preference,
			DiskSizeArtifactKey:                  diskSize,
		},
	}
}

func (s *AppContext) Put(key StateBagEntry, value interface{}) {
	s.State.Put(string(key), value)
}

func (s *AppContext) get(key StateBagEntry) interface{} {
	return s.State.Get(string(key))
}

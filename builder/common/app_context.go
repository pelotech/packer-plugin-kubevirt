package common

import (
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	kubevirtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1alpha1"
	"packer-plugin-kubevirt/builder/common/vm"
)

type StateBagEntry string

const (
	PackerHook                StateBagEntry = "hook"
	PackerUi                  StateBagEntry = "ui"
	PackerError               StateBagEntry = "error"
	VirtualMachine            StateBagEntry = "vm"
	VirtualMachineOsFamily    StateBagEntry = "vmosfamily"
	VirtualMachineExport      StateBagEntry = "vmexport"
	VirtualMachineExportToken StateBagEntry = "vmexporttoken"

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

func (s *AppContext) GetVirtualMachine() *kubevirtv1.VirtualMachine {
	vm := s.get(VirtualMachine)
	if vm != nil {
		return vm.(*kubevirtv1.VirtualMachine)
	}
	return nil
}

func (s *AppContext) GetVirtualMachineOSFamily() *vm.OsFamily {
	osFamily := s.get(VirtualMachineOsFamily)
	if osFamily != nil {
		return osFamily.(*vm.OsFamily)
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

func (s *AppContext) BuildArtifact(builderId string) packersdk.Artifact {
	return &KubevirtArtifact{
		BuilderIdValue: builderId,
		StateData: map[string]interface{}{
			NamespaceArtifactKey:                 s.GetVirtualMachineExport().Namespace,
			VirtualMachineExportNameArtifactKey:  s.GetVirtualMachineExport().Name,
			VirtualMachineExportTokenArtifactKey: s.GetVirtualMachineExportToken(),
		},
	}
}

func (s *AppContext) Put(key StateBagEntry, value interface{}) {
	s.State.Put(string(key), value)
}

func (s *AppContext) get(key StateBagEntry) interface{} {
	return s.State.Get(string(key))
}

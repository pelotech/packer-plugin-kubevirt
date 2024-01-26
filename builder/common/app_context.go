package common

import (
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	kubevirtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1alpha1"
	"packer-plugin-kubevirt/builder/iso"
)

type StateBagEntry string

const (
	PackerHook  StateBagEntry = "hook"
	PackerUi    StateBagEntry = "ui"
	PackerError StateBagEntry = "error"

	VirtualMachine            StateBagEntry = "vm"
	VirtualMachineExport      StateBagEntry = "vmexport"
	VirtualMachineExportToken StateBagEntry = "vmexporttoken"
)

type AppContext struct {
	State multistep.StateBag
}

func (s *AppContext) GetPackerError() error {
	return s.get(PackerError).(error)
}

func (s *AppContext) GetPackerUi() packersdk.Ui {
	return s.get(PackerUi).(packersdk.Ui)
}

func (s *AppContext) GetVirtualMachine() kubevirtv1.VirtualMachine {
	return s.get(VirtualMachine).(kubevirtv1.VirtualMachine)
}

func (s *AppContext) GetVirtualMachineExport() exportv1.VirtualMachineExport {
	return s.get(VirtualMachineExport).(exportv1.VirtualMachineExport)
}

func (s *AppContext) GetVirtualMachineExportToken() exportv1.VirtualMachineExport {
	return s.get(VirtualMachineExportToken).(exportv1.VirtualMachineExport)
}

func (s *AppContext) BuildArtifact() packersdk.Artifact {
	return &iso.Artifact{
		StateData: map[string]interface{}{
			string(VirtualMachine):            s.GetVirtualMachine(),
			string(VirtualMachineExport):      s.GetVirtualMachineExport(),
			string(VirtualMachineExportToken): s.GetVirtualMachineExportToken(),
		},
	}
}

func (s *AppContext) Put(key StateBagEntry, value interface{}) {
	s.State.Put(string(key), value)
}

func (s *AppContext) get(key StateBagEntry) interface{} {
	return s.State.Get(string(key))
}

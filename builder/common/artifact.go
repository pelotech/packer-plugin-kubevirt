// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

const (
	NamespaceArtifactKey                 = "namespace"
	VirtualMachineExportNameArtifactKey  = "vmexport"
	VirtualMachineExportTokenArtifactKey = "token"
)

// KubevirtArtifact packersdk.KubevirtArtifact implementation
type KubevirtArtifact struct {
	// BuilderId is the unique ID for the builder that created this VM Image
	BuilderIdValue string
	// StateData should store data such as GeneratedData
	// to be common with post-processors
	StateData map[string]interface{}
}

func (a *KubevirtArtifact) BuilderId() string {
	return a.BuilderIdValue
}

func (a *KubevirtArtifact) Files() []string {
	return []string{}
}

func (*KubevirtArtifact) Id() string {
	return ""
}

func (a *KubevirtArtifact) String() string {
	return ""
}

func (a *KubevirtArtifact) State(name string) interface{} {
	return a.StateData[name]
}

func (a *KubevirtArtifact) Destroy() error {
	return nil
}

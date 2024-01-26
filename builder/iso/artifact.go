// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package iso

const (
	InternalUrlArtifact  = "internal_url"
	InternalCertArtifact = "internal_cert"
)

// Artifact packersdk.Artifact implementation
type Artifact struct {
	// StateData should store data such as GeneratedData
	// to be common with post-processors
	StateData map[string]interface{}
}

func (*Artifact) BuilderId() string {
	return BuilderId
}

func (a *Artifact) Files() []string {
	return []string{}
}

func (*Artifact) Id() string {
	return ""
}

func (a *Artifact) String() string {
	return ""
}

func (a *Artifact) State(name string) interface{} {
	return a.StateData[name]
}

func (a *Artifact) Destroy() error {
	return nil
}

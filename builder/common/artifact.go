// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

// Artifact packersdk.Artifact implementation
type Artifact struct {
	// BuilderId is the unique ID for the builder that created this VM Image
	BuilderIdValue string
	// StateData should store data such as GeneratedData
	// to be common with post-processors
	StateData map[string]interface{}
}

func (a *Artifact) BuilderId() string {
	return a.BuilderIdValue
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

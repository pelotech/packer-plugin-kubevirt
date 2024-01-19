package steps

import (
	"context"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
)

type StepConvertVM struct {
}

func (s *StepConvertVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	// TODO: current is VM export downloaded (init c.) -> uploaded (c.)
	// post-download, it should be converted (e.g. qcow2) if an output conversion format is set.
	// a RWX PVC could be used or S3 csi?

	return multistep.ActionContinue
}

func (s *StepConvertVM) Cleanup(_ multistep.StateBag) {
	// Cleaning up 'Virtual Machine Conversion' during the build would prevent any post-processor to download the export
}

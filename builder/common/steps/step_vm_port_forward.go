package steps

import (
	"context"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"kubevirt.io/client-go/kubecli"
)

type StepPortForwardVM struct {
	VirtClient kubecli.KubevirtClient
}

func (s *StepPortForwardVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	return multistep.ActionContinue
}

func (s *StepPortForwardVM) Cleanup(_ multistep.StateBag) {
}

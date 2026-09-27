package steps

import (
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"net"
	"testing"
)

func TestPortMappingsUseFreeLocalPort(t *testing.T) {
	for commType, remotePort := range map[string]int{"ssh": 22, "winrm": 5985} {
		comm := &communicator.Config{Type: commType}
		step := &StepPortForwardVM{Comm: comm}

		portMappings, err := step.computePortMappings()
		if err != nil {
			t.Fatalf("expected port mappings for '%s', got: %v", commType, err)
		}

		localPort := comm.Port()
		if localPort < 1024 {
			t.Fatalf("expected a local port above 1023 for '%s', got: %d", commType, localPort)
		}
		expected := fmt.Sprintf("%d:%d", localPort, remotePort)
		if len(portMappings) != 1 || portMappings[0] != expected {
			t.Errorf("expected port mapping '%s', got: %v", expected, portMappings)
		}
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
		if err != nil {
			t.Fatalf("expected local port %d to be free, got: %v", localPort, err)
		}
		_ = listener.Close()
	}
}

func TestPortMappingsKeepLocalPortOfTheUser(t *testing.T) {
	tests := map[string]*communicator.Config{
		"2200:22":   {Type: "ssh", SSH: communicator.SSH{SSHPort: 2200}},
		"5900:5985": {Type: "winrm", WinRM: communicator.WinRM{WinRMPort: 5900}},
	}
	for expected, comm := range tests {
		step := &StepPortForwardVM{Comm: comm}

		portMappings, err := step.computePortMappings()
		if err != nil {
			t.Fatalf("expected port mapping '%s', got: %v", expected, err)
		}
		if len(portMappings) != 1 || portMappings[0] != expected {
			t.Errorf("expected port mapping '%s', got: %v", expected, portMappings)
		}
	}
}

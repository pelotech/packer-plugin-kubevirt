package common

import (
	"fmt"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"log"
	"net"
	"os"
	"strings"
)

func GetEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return defaultValue
	}
	return value
}

func IsReservedPort(value int) bool {
	return value > 0 && value < 1024
}

func FindFreePort() (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(VirtualMachineHost, "0"))
	if err != nil {
		return 0, fmt.Errorf("failed to find a free local port: %w", err)
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port, nil
}

func AskForRecreation(ui packersdk.Ui, deleteFunc func() error) error {
	for {
		line, err := ui.Ask("[r] recreate resource, [c] continue and let it fail")
		if err != nil {
			log.Printf("Error asking for input: %s", err)
		}

		input := strings.ToLower(line) + "c"
		switch input[0] {
		case 'r':
			return deleteFunc()
		case 'c':
			return nil
		default:
			ui.Error("incorrect input, valid inputs: 'r', 'c'")
			return AskForRecreation(ui, deleteFunc)
		}
	}
}

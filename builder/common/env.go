package common

import (
	"fmt"
	"net"
)

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

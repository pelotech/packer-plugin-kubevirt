package common

import (
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"log"
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

func GetOrDefault(value, defaultValue int) int {
	if value == 0 {
		return defaultValue
	}
	return value
}

func IsReservedPort(value int) bool {
	return value > 0 && value < 1024
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

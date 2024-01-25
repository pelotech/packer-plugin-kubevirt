package utils

import (
	"log"
	"os"
	"path/filepath"
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

func ReadFile(path, filename string) string {
	scriptData, err := os.ReadFile(filepath.Join(path, filename))
	if err != nil {
		log.Printf("error reading file: %s", err)
	}

	return string(scriptData)
}

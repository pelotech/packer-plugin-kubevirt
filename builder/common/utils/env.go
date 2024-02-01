package utils

import (
	"os"
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

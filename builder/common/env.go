package common

func GetOrDefault(value, defaultValue int) int {
	if value == 0 {
		return defaultValue
	}
	return value
}

func IsReservedPort(value int) bool {
	return value > 0 && value < 1024
}

package vm

import "strings"

type OsFamily int32

const (
	Linux   OsFamily = 0
	Windows          = 1
)

func GetOSFamily(preference string) OsFamily {
	if strings.Contains(strings.ToLower(preference), "windows") {
		return Windows
	}
	return Linux
}

package common

import (
	mathrandom "math/rand"
)

func GenerateRandomPassword(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	b := make([]rune, n)
	for i := range b {
		b[i] = letters[mathrandom.Intn(len(letters))]
	}
	return string(b)
}

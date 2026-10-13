package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

func GenerateTenantCode() (string, error) {
	const (
		codeLength = 8
		characters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	)
	code := make([]byte, codeLength)
	for index := range code {
		value, err := rand.Int(rand.Reader, big.NewInt(int64(len(characters))))
		if err != nil {
			return "", fmt.Errorf("generate tenant code: %w", err)
		}
		code[index] = characters[value.Int64()]
	}
	return string(code), nil
}

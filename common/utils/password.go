package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

const passwordCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"

// GenerateRandomPassword returns a cryptographically random password of the
// requested length drawn from passwordCharset, with no modulo bias. Returns
// an error when the system entropy source is unavailable.
func GenerateRandomPassword(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("password length must be positive")
	}
	max := big.NewInt(int64(len(passwordCharset)))
	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("password rand: %w", err)
		}
		out[i] = passwordCharset[n.Int64()]
	}
	return string(out), nil
}

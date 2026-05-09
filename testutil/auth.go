package testutil

import (
	"github.com/golang-jwt/jwt/v5"
)

// GenerateTestToken generates a JWT token for testing
func GenerateTestToken(username, role string) (string, error) {
	claims := jwt.MapClaims{
		"username": username,
		"role":     role,
		"exp":      9999999999,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte("test-secret"))
}

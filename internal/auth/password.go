package auth

import (
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const passwordCost = 12

func HashPassword(password string) (string, error) {
	length := utf8.RuneCountInString(password)
	if length < 12 {
		return "", fmt.Errorf("password must contain at least 12 characters")
	}
	if len(password) > 72 {
		return "", fmt.Errorf("password must not exceed 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

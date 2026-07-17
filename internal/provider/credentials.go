package provider

import (
	"fmt"
	"strings"

	"github.com/XR-Lee/Gemcp/internal/secrets"
)

const CredentialAAD = "gemcp:provider-token:v1"

func EncryptCredential(box *secrets.Box, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", &ValidationError{Message: "Provider token is required"}
	}
	plaintext := []byte(token)
	defer func() {
		for i := range plaintext {
			plaintext[i] = 0
		}
	}()
	ciphertext, err := box.Encrypt(plaintext, CredentialAAD)
	if err != nil {
		return "", fmt.Errorf("encrypt Provider token: %w", err)
	}
	return ciphertext, nil
}

func DecryptCredential(box *secrets.Box, ciphertext string) (string, error) {
	plaintext, err := box.Decrypt(ciphertext, CredentialAAD)
	if err != nil {
		return "", fmt.Errorf("decrypt Provider token: %w", err)
	}
	defer func() {
		for i := range plaintext {
			plaintext[i] = 0
		}
	}()
	token := strings.TrimSpace(string(plaintext))
	if token == "" {
		return "", fmt.Errorf("decrypted Provider token is empty")
	}
	return token, nil
}

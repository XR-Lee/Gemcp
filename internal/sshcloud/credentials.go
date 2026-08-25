package sshcloud

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/crypto/ssh"

	"github.com/XR-Lee/Gemcp/internal/secrets"
)

func EncryptCredential(box *secrets.Box, credential Credential) (string, error) {
	normalized, err := normalizeCredential(credential)
	if err != nil {
		return "", err
	}
	plaintext, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("encode Cloud SSH credential: %w", err)
	}
	defer zeroBytes(plaintext)
	ciphertext, err := box.Encrypt(plaintext, CredentialAAD)
	if err != nil {
		return "", fmt.Errorf("encrypt Cloud SSH credential: %w", err)
	}
	return ciphertext, nil
}

func DecryptCredential(box *secrets.Box, ciphertext string) (Credential, error) {
	plaintext, err := box.Decrypt(ciphertext, CredentialAAD)
	if err != nil {
		return Credential{}, fmt.Errorf("decrypt Cloud SSH credential: %w", err)
	}
	defer zeroBytes(plaintext)
	var credential Credential
	if err := json.Unmarshal(plaintext, &credential); err != nil {
		return Credential{}, fmt.Errorf("decode Cloud SSH credential: %w", err)
	}
	return normalizeCredential(credential)
}

func normalizeCredential(credential Credential) (Credential, error) {
	method := strings.ToLower(strings.TrimSpace(credential.Method))
	switch method {
	case "password":
		password := strings.TrimSpace(credential.Password)
		if password == "" || len(password) > 1024 {
			return Credential{}, invalid("SSH password is required")
		}
		for _, character := range password {
			if unicode.IsControl(character) {
				return Credential{}, invalid("SSH password contains a control character")
			}
		}
		return Credential{Method: method, Password: password}, nil
	case "private_key":
		key := strings.TrimSpace(credential.PrivateKey)
		if key == "" || len(key) > 16<<10 {
			return Credential{}, invalid("SSH private key is required")
		}
		passphrase := strings.TrimSpace(credential.Passphrase)
		if passphrase != "" && len(passphrase) > 1024 {
			return Credential{}, invalid("SSH key passphrase is too long")
		}
		if err := validatePrivateKey(key, passphrase); err != nil {
			return Credential{}, err
		}
		return Credential{Method: method, PrivateKey: key, Passphrase: passphrase}, nil
	default:
		return Credential{}, invalid("auth_method must be password or private_key")
	}
}

func validatePrivateKey(key, passphrase string) error {
	var err error
	if passphrase == "" {
		_, err = ssh.ParsePrivateKey([]byte(key))
	} else {
		_, err = ssh.ParsePrivateKeyWithPassphrase([]byte(key), []byte(passphrase))
	}
	if err != nil {
		return invalid("SSH private key could not be parsed")
	}
	return nil
}

func authMethods(credential Credential) ([]ssh.AuthMethod, error) {
	normalized, err := normalizeCredential(credential)
	if err != nil {
		return nil, err
	}
	switch normalized.Method {
	case "password":
		return []ssh.AuthMethod{ssh.Password(normalized.Password)}, nil
	case "private_key":
		var signer ssh.Signer
		if normalized.Passphrase == "" {
			signer, err = ssh.ParsePrivateKey([]byte(normalized.PrivateKey))
		} else {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(normalized.PrivateKey), []byte(normalized.Passphrase))
		}
		if err != nil {
			return nil, invalid("SSH private key could not be parsed")
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default:
		return nil, invalid("auth_method must be password or private_key")
	}
}

func zeroBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

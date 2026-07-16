package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const (
	keySize       = 32
	cipherVersion = "v1"
)

type Box struct {
	aead cipher.AEAD
	key  []byte
}

func New(encodedKey string) (*Box, error) {
	key, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(encodedKey))
	if err != nil {
		key, err = base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey))
	}
	if err != nil {
		return nil, fmt.Errorf("decode Gemcp master key: %w", err)
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("Gemcp master key must decode to %d bytes", keySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create AES-GCM: %w", err)
	}
	return &Box{aead: aead, key: append([]byte(nil), key...)}, nil
}

func GenerateMasterKey() (string, error) {
	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("generate master key: %w", err)
	}
	return base64.RawStdEncoding.EncodeToString(key), nil
}

func (b *Box) Encrypt(plaintext []byte, associatedData string) (string, error) {
	if b == nil || b.aead == nil {
		return "", fmt.Errorf("secret box is not initialized")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate encryption nonce: %w", err)
	}
	sealed := b.aead.Seal(nil, nonce, plaintext, []byte(associatedData))
	payload := append(nonce, sealed...)
	return cipherVersion + "." + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (b *Box) Decrypt(encoded, associatedData string) ([]byte, error) {
	if b == nil || b.aead == nil {
		return nil, fmt.Errorf("secret box is not initialized")
	}
	parts := strings.SplitN(encoded, ".", 2)
	if len(parts) != 2 || parts[0] != cipherVersion {
		return nil, fmt.Errorf("unsupported encrypted secret format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode encrypted secret: %w", err)
	}
	if len(payload) < b.aead.NonceSize() {
		return nil, fmt.Errorf("encrypted secret is truncated")
	}
	nonce := payload[:b.aead.NonceSize()]
	ciphertext := payload[b.aead.NonceSize():]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, []byte(associatedData))
	if err != nil {
		return nil, fmt.Errorf("decrypt secret: authentication failed")
	}
	return plaintext, nil
}

func (b *Box) Digest(namespace, value string) []byte {
	mac := hmac.New(sha256.New, b.key)
	_, _ = mac.Write([]byte(namespace))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func RandomToken(prefix string, randomBytes int) (plain, displayPrefix string, err error) {
	if randomBytes < 16 {
		return "", "", fmt.Errorf("token entropy must be at least 16 bytes")
	}
	raw := make([]byte, randomBytes)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)
	identifierRaw := make([]byte, 5)
	if _, err := io.ReadFull(rand.Reader, identifierRaw); err != nil {
		return "", "", fmt.Errorf("generate token prefix: %w", err)
	}
	displayPrefix = prefix + "_" + base64.RawURLEncoding.EncodeToString(identifierRaw)
	return displayPrefix + "_" + secret, displayPrefix, nil
}

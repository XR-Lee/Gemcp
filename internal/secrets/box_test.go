package secrets

import (
	"bytes"
	"strings"
	"testing"
)

func testBox(t *testing.T) *Box {
	t.Helper()
	key, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey() error = %v", err)
	}
	box, err := New(key)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return box
}

func TestEncryptRoundTrip(t *testing.T) {
	box := testBox(t)
	ciphertext, err := box.Encrypt([]byte("provider-secret"), "provider-token")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if strings.Contains(ciphertext, "provider-secret") {
		t.Fatal("ciphertext contains plaintext")
	}
	plaintext, err := box.Decrypt(ciphertext, "provider-token")
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if string(plaintext) != "provider-secret" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}

func TestAssociatedDataMismatchFails(t *testing.T) {
	box := testBox(t)
	ciphertext, _ := box.Encrypt([]byte("secret"), "scope-a")
	if _, err := box.Decrypt(ciphertext, "scope-b"); err == nil {
		t.Fatal("Decrypt() accepted wrong associated data")
	}
}

func TestDigestNamespaces(t *testing.T) {
	box := testBox(t)
	first := box.Digest("agent", "same")
	second := box.Digest("session", "same")
	if bytes.Equal(first, second) {
		t.Fatal("Digest() ignored namespace")
	}
}

func TestRandomToken(t *testing.T) {
	plain, prefix, err := RandomToken("gmc", 32)
	if err != nil {
		t.Fatalf("RandomToken() error = %v", err)
	}
	if !strings.HasPrefix(plain, prefix+"_") || !strings.HasPrefix(prefix, "gmc_") {
		t.Fatalf("plain=%q prefix=%q", plain, prefix)
	}
}

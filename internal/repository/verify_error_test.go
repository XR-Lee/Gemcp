package repository

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyVerifyFailure(t *testing.T) {
	t.Parallel()
	if got := ClassifyVerifyFailure(errors.New("ERROR: Deploy keys are disabled for this repository")); got != VerifyFailureDeployKeysDisabled {
		t.Fatalf("disabled = %q", got)
	}
	if got := ClassifyVerifyFailure(errors.New("Permission denied (publickey)")); got != VerifyFailureDeployKeyMissing {
		t.Fatalf("publickey = %q", got)
	}
	if got := ClassifyVerifyFailure(errors.New("ssh: handshake failed")); got != VerifyFailureGeneric {
		t.Fatalf("generic = %q", got)
	}
}

func TestWrapVerifyErrorSurfacesDeployKeysDisabled(t *testing.T) {
	t.Parallel()
	err := WrapVerifyError(errors.New("ERROR: Deploy keys are disabled for this repository"))
	if !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("wrap is not ErrVerificationFailed: %v", err)
	}
	if !strings.Contains(err.Error(), "GitHub Deploy Keys are disabled") || !strings.Contains(err.Error(), ObservationWritesPendingNote) {
		t.Fatalf("wrap message = %q", err)
	}
	if !strings.Contains(err.Error(), "https_token") || !strings.Contains(err.Error(), "Contents: Read") {
		t.Fatalf("wrap should point at HTTPS token: %q", err)
	}
	if VerifyFailureCode(err) != "REPOSITORY_DEPLOY_KEYS_DISABLED" {
		t.Fatalf("code = %q", VerifyFailureCode(err))
	}
}

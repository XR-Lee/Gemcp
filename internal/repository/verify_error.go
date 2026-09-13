package repository

import (
	"fmt"
	"strings"
)

const (
	VerifyFailureDeployKeysDisabled = "deploy_keys_disabled"
	VerifyFailureDeployKeyMissing   = "deploy_key_missing"
	VerifyFailureGeneric            = "verification_failed"
)

func ClassifyVerifyFailure(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "deploy keys are disabled") ||
		strings.Contains(msg, "deploy key is disabled") ||
		strings.Contains(msg, "deploy keys disabled") ||
		(strings.Contains(msg, "deploy key") && strings.Contains(msg, "disabled")) ||
		(strings.Contains(msg, "organization policy") && strings.Contains(msg, "deploy")) {
		return VerifyFailureDeployKeysDisabled
	}
	if strings.Contains(msg, "permission denied (publickey)") ||
		strings.Contains(msg, "no matching host key") && strings.Contains(msg, "publickey") {
		return VerifyFailureDeployKeyMissing
	}
	if strings.Contains(msg, "permission denied") && strings.Contains(msg, "publickey") {
		return VerifyFailureDeployKeyMissing
	}
	return VerifyFailureGeneric
}

func WrapVerifyError(err error) error {
	if err == nil {
		return nil
	}
	switch ClassifyVerifyFailure(err) {
	case VerifyFailureDeployKeysDisabled:
		return fmt.Errorf("%w: GitHub Deploy Keys are disabled for this repository or organization. %s Ask a repository administrator to enable Deploy Keys, or register a public HTTPS repository.", ErrVerificationFailed, ObservationWritesPendingNote)
	case VerifyFailureDeployKeyMissing:
		return fmt.Errorf("%w: GitHub rejected this Deploy Key (permission denied). Install the Gemcp public key as a read-only Deploy Key, then verify again. %s", ErrVerificationFailed, ObservationWritesPendingNote)
	default:
		return fmt.Errorf("%w: %v. %s", ErrVerificationFailed, err, ObservationWritesPendingNote)
	}
}

func VerifyFailureCode(err error) string {
	switch ClassifyVerifyFailure(err) {
	case VerifyFailureDeployKeysDisabled:
		return "REPOSITORY_DEPLOY_KEYS_DISABLED"
	case VerifyFailureDeployKeyMissing:
		return "REPOSITORY_DEPLOY_KEY_MISSING"
	default:
		return "REPOSITORY_VERIFICATION_FAILED"
	}
}

package repository

import (
	"fmt"
	"strings"
)

const (
	VerifyFailureDeployKeysDisabled = "deploy_keys_disabled"
	VerifyFailureDeployKeyMissing   = "deploy_key_missing"
	VerifyFailureHTTPSTokenInvalid  = "https_token_invalid"
	VerifyFailureGeneric            = "verification_failed"
)

const httpsTokenConfigureHint = "Configure a GitHub fine-grained personal access token with Contents: Read on this one repository, then call verify_repository with write-only https_token. That activates access=https_token without Deploy Keys."

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
	if strings.Contains(msg, "authentication failed") ||
		strings.Contains(msg, "invalid username or password") ||
		strings.Contains(msg, "invalid username or token") ||
		strings.Contains(msg, "bad credentials") ||
		strings.Contains(msg, "rejected the https token") {
		return VerifyFailureHTTPSTokenInvalid
	}
	return VerifyFailureGeneric
}

func WrapVerifyError(err error) error {
	if err == nil {
		return nil
	}
	switch ClassifyVerifyFailure(err) {
	case VerifyFailureDeployKeysDisabled:
		return fmt.Errorf("%w: GitHub Deploy Keys are disabled for this repository or organization. %s %s", ErrVerificationFailed, httpsTokenConfigureHint, ObservationWritesPendingNote)
	case VerifyFailureDeployKeyMissing:
		return fmt.Errorf("%w: GitHub rejected this Deploy Key (permission denied). Install the Gemcp public key as a read-only Deploy Key, then verify again. If Deploy Keys are unavailable, %s %s", ErrVerificationFailed, httpsTokenConfigureHint, ObservationWritesPendingNote)
	case VerifyFailureHTTPSTokenInvalid:
		return fmt.Errorf("%w: GitHub rejected the HTTPS token. Use a fine-grained token with Contents: Read on this repository and pass it once as https_token. %s", ErrVerificationFailed, ObservationWritesPendingNote)
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
	case VerifyFailureHTTPSTokenInvalid:
		return "REPOSITORY_HTTPS_TOKEN_INVALID"
	default:
		return "REPOSITORY_VERIFICATION_FAILED"
	}
}

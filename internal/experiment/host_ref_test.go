package experiment

import "testing"

func TestPublicGitTokenHidesHostSentinel(t *testing.T) {
	t.Parallel()
	if got := publicGitToken("host"); got != "" {
		t.Fatalf("publicGitToken(host) = %q", got)
	}
	if got := publicGitToken("HOST"); got != "" {
		t.Fatalf("publicGitToken(HOST) = %q", got)
	}
	sha := "0123456789012345678901234567890123456789"
	if got := publicGitToken(sha); got != sha {
		t.Fatalf("publicGitToken(sha) = %q", got)
	}
}

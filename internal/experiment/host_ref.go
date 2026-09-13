package experiment

import "github.com/XR-Lee/Gemcp/internal/sshcloud"

// publicGitToken hides the host-process sentinel from Owner/Agent views.
// The stored proposal/experiment value may still be sshcloud.HostCommit.
func publicGitToken(value string) string {
	if sshcloud.IsHostSentinel(value) {
		return ""
	}
	return value
}

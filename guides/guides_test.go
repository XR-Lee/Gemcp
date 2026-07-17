package guides

import (
	"strings"
	"testing"
)

func TestEmbeddedGuidesContainSafetyWorkflow(t *testing.T) {
	for name, guide := range map[string]string{"agent": AgentMCP(), "owner": OwnerMCP()} {
		for _, required := range []string{"get_project_options", "get_project_cost", "idempotency", "Agent Token"} {
			if !strings.Contains(guide, required) {
				t.Fatalf("%s guide does not contain %q", name, required)
			}
		}
		if strings.Contains(guide, "gmc_") || strings.Contains(guide, "Bearer gmc") {
			t.Fatalf("%s guide contains a token-shaped example", name)
		}
	}
	if !strings.Contains(AgentMCP(), "Wait for explicit human approval") {
		t.Fatal("Agent guide omits the paid-work approval boundary")
	}
}

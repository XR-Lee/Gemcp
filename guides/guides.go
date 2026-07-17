package guides

import (
	_ "embed"
	"strings"
)

const (
	AgentResourceURI = "gemcp://docs/agent-guide"
	AgentPromptName  = "operate_gemcp"
)

//go:embed agent-mcp.md
var agentMCP string

//go:embed owner-mcp.md
var ownerMCP string

func AgentMCP() string { return strings.TrimSpace(agentMCP) + "\n" }

func OwnerMCP() string { return strings.TrimSpace(ownerMCP) + "\n" }

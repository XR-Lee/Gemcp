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

//go:embed pi-setup.md
var piSetup string

//go:embed pi-setup-installer.mjs
var piSetupInstaller string

//go:embed gemcp-tool.mjs
var gemcpTool string

func AgentMCP() string { return strings.TrimSpace(agentMCP) + "\n" }

func OwnerMCP() string { return strings.TrimSpace(ownerMCP) + "\n" }

func PiSetup(publicURL string) string { return renderPublicURL(piSetup, publicURL) }

func PiSetupInstaller(publicURL string) string { return renderPublicURL(piSetupInstaller, publicURL) }

func GemcpTool() string { return strings.TrimSpace(gemcpTool) + "\n" }

func renderPublicURL(content, publicURL string) string {
	origin := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	return strings.ReplaceAll(strings.TrimSpace(content)+"\n", "{{GEMCP_PUBLIC_URL}}", origin)
}

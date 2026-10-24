package researchbench

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPTools calls Gemcp tools over an MCP client session.
type MCPTools struct{ Session *mcp.ClientSession }

func (m MCPTools) Call(ctx context.Context, name string, arguments map[string]any) (map[string]any, error) {
	result, err := m.Session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if result.IsError {
		var message []string
		for _, content := range result.Content {
			if typed, ok := content.(*mcp.TextContent); ok {
				message = append(message, typed.Text)
			}
		}
		return nil, fmt.Errorf("%s: %s", name, strings.Join(message, " "))
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, fmt.Errorf("%s returned no JSON object: %w", name, err)
	}
	return decoded, nil
}

// Connect opens an MCP session to a Gemcp endpoint with an Agent token, for
// scoring a Study on a live control plane.
func Connect(ctx context.Context, endpoint, token string) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "gemcp-research-bench", Version: "1"}, nil)
	return client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: &http.Client{Transport: bearer{token: token, base: http.DefaultTransport}},
	}, nil)
}

type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(clone)
}

# Engineering references

Gemcp is an independent implementation. These projects inform specific design choices; none is a codebase to fork.

- Sub2API: Go/Gin/Ent layering, embedded Vue delivery, first-run setup, and Docker deployment.
- cyicz123/autodl-skills: AutoDL API endpoint coverage and contract-test cases.
- musharna/jobd: asynchronous job states, MCP surface, heartbeat, cancellation, and terminal-state discipline.
- Hanyuyuan6/remote-gpu-trainer: runner, checkpoint, cache, and teardown operating practices.
- kyuwon-shim-ARL/runpod-mcp: independent provider watchdog pattern and its durability limitations.

- modelcontextprotocol/go-sdk `v1.6.1`: official MCP Streamable HTTP server, typed tool schemas, Bearer middleware, and authenticated session binding.
- Claude Code remote HTTP MCP configuration: `https://docs.anthropic.com/en/docs/claude-code/mcp`.
- Cursor MCP JSON and environment interpolation: `https://cursor.com/docs/mcp`.
- VS Code MCP server configuration: `https://code.visualstudio.com/docs/copilot/chat/mcp-servers`.
- OpenAI Codex Streamable HTTP and bearer-token configuration: `https://developers.openai.com/codex/mcp`.
- Grok Build project MCP configuration: `https://docs.x.ai/build/features/mcp-servers`.
- AutoDL Public Elastic Developer API: `https://www.autodl.com/docs/esd_api_doc/`.
- AutoDL common Developer API, including public wallet balance: `https://www.autodl.com/docs/common_api/`.
- AutoDL Private Cloud Developer API: `https://private.autodl.com/docs/esd_api_doc/`.
- AutoDL Private Cloud image and storage behavior: `https://private.autodl.com/docs/image/` and `https://private.autodl.com/docs/fs/`.
- golang.org/x/crypto/ssh: outbound Cloud SSH client, host-key fingerprints, and private-key parsing.

Production behavior is validated against current official AutoDL documentation and live phase-zero probes.

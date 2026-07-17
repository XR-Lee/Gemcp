# Engineering references

Gemcp is an independent implementation. These projects inform specific design choices; none is a codebase to fork.

- Sub2API: Go/Gin/Ent layering, embedded Vue delivery, first-run setup, and Docker deployment.
- cyicz123/autodl-skills: AutoDL API endpoint coverage and contract-test cases.
- musharna/jobd: asynchronous job states, MCP surface, heartbeat, cancellation, and terminal-state discipline.
- Hanyuyuan6/remote-gpu-trainer: runner, checkpoint, cache, and teardown operating practices.
- kyuwon-shim-ARL/runpod-mcp: independent provider watchdog pattern and its durability limitations.

- modelcontextprotocol/go-sdk `v1.6.1`: official MCP Streamable HTTP server, typed tool schemas, Bearer middleware, and authenticated session binding.
- AutoDL Private Cloud Developer API: `https://private.autodl.com/docs/esd_api_doc/`.
- AutoDL Private Cloud image and storage behavior: `https://private.autodl.com/docs/image/` and `https://private.autodl.com/docs/fs/`.

Production behavior is validated against current official AutoDL documentation and live phase-zero probes.

# Third-party MCP clients

Gemcp exposes the official MCP Streamable HTTP transport at:

```text
https://<gemcp-host>/mcp
```

Every request requires a project-scoped Agent Token:

```http
Authorization: Bearer gmc_<identifier>_<secret>
```

For Pi, create a short-lived **Pi setup link** from the Owner console and let the Agent install its own credential. For other clients, create a Token from the advanced console action or the [Agent access API](agent-tokens.md). Gemcp stores only HMAC-SHA-256 digests. Revocation and expiration are checked on every HTTP request, and the authenticated Token identity is bound to the MCP session.

## Pi with pi-mcp-adapter

Gemcp's preferred Pi flow requires the already-installed `pi-mcp-adapter`. The Owner sends one URL from `/agent/setup#code=...`; the Agent reads the public setup instructions and runs the fixed installer from the same configured origin. The code remains in the URL fragment and is not sent by link previews or ordinary page requests.

The installer merges a `gemcp-<project>` server into `<Pi agent dir>/mcp.json`, preserves existing servers, writes mode `0600`, exposes all eleven bounded Gemcp tools through `directTools`, and verifies tool discovery plus guide, options, and cost calls. A local credential-reading helper supports the current session without printing the Token. One `/reload` activates native `gemcp-<project>_*` tools through the adapter.

Claimed credentials remain `read`-only and expire at the setup deadline until verification completes. Completion activates the Owner-selected scopes and lifetime, clears the setup capability, and leaves only a credential-free local receipt. The complete API and installer are retry-safe if the final response is lost.

## Built-in operating guides

The production service serves two non-secret Markdown documents from its configured `GEMCP_PUBLIC_URL`:

```text
https://<gemcp-host>/docs/owner-mcp.md
https://<gemcp-host>/docs/agent-mcp.md
```

The Owner guide covers client setup, scope selection, approval policy, verification, rotation, and incident response. The Agent guide is the handoff document: download or copy it separately after configuring the MCP client. Do not append the Agent Token to either document or paste the Token into an Agent prompt.

MCP clients can discover the same Agent guide through all three capability levels:

- Tool: `get_usage_guide` for clients that primarily expose Tools.
- Resource: `gemcp://docs/agent-guide` with MIME type `text/markdown`.
- Prompt: `operate_gemcp`, which loads the safe operating workflow as a user message.

The server's initialization instructions also summarize the mandatory preflight and approval boundary. The Markdown files in [`guides/`](../guides/) are the embedded source used by the HTTP routes, Tool, Resource, Prompt, and Owner console download.

## Exported JSON

The one-time download uses the `mcpServers` format accepted by Claude Code and Cursor:

```json
{
  "mcpServers": {
    "gemcp-project": {
      "type": "http",
      "url": "https://gemcp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer gmc_abcd123_<one-time-secret>"
      }
    }
  }
}
```

This file contains a live secret. Do not commit it, attach it to an issue, or send it through chat. Import it into the intended client and then move the Token to that client's secret or environment-variable mechanism.

The Agents page also exposes a non-secret template:

```json
{
  "mcpServers": {
    "gemcp-project": {
      "type": "http",
      "url": "https://gemcp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${GEMCP_AGENT_TOKEN}"
      }
    }
  }
}
```

Environment interpolation syntax differs by client. Use the matching configuration below rather than assuming one placeholder syntax is universal.

## Claude Code

Claude Code accepts remote HTTP servers in `.mcp.json`, `~/.claude.json`, or through `claude mcp add-json`. For project configuration, create `.mcp.json`:

```json
{
  "mcpServers": {
    "gemcp-project": {
      "type": "http",
      "url": "https://gemcp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${GEMCP_AGENT_TOKEN}"
      }
    }
  }
}
```

Set the secret in the environment that launches Claude Code:

```bash
export GEMCP_AGENT_TOKEN='gmc_abcd123_<secret>'
claude mcp list
```

Start Claude Code, approve the project MCP server when prompted, and open `/mcp`. The server should report connected and expose the Gemcp tools. Claude Code requires `type: "http"` or `type: "streamable-http"` when a remote `url` is present.

For a personal credential that must not enter source control, use Claude's local or user scope instead of committing an inline bearer header.

## Cursor

Cursor reads project configuration from `.cursor/mcp.json` and global configuration from `~/.cursor/mcp.json`. Cursor's environment syntax is `${env:NAME}`:

```json
{
  "mcpServers": {
    "gemcp-project": {
      "type": "http",
      "url": "https://gemcp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${env:GEMCP_AGENT_TOKEN}"
      }
    }
  }
}
```

Set `GEMCP_AGENT_TOKEN` before launching Cursor, open **Customize**, select the server, and verify that its tools are available. Use **MCP Logs** in Cursor's Output panel for HTTP or authentication failures.

The downloaded one-time JSON can be opened directly as Cursor `mcp.json`, but it embeds the Token and must not be committed.

## Visual Studio Code

VS Code uses a different top-level key in `.vscode/mcp.json` or the user-profile MCP configuration. Use a password input so the Token is not stored in the workspace file:

```json
{
  "inputs": [
    {
      "type": "promptString",
      "id": "gemcpAgentToken",
      "description": "Gemcp Agent Token",
      "password": true
    }
  ],
  "servers": {
    "gemcp-project": {
      "type": "http",
      "url": "https://gemcp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${input:gemcpAgentToken}"
      }
    }
  }
}
```

Run **MCP: List Servers**, start `gemcp-project`, enter the Token, and confirm trust. VS Code then lists Gemcp tools in Chat's tool picker. The generic download cannot be used unchanged because VS Code expects `servers`, not `mcpServers`.

## OpenAI Codex and ChatGPT desktop

Codex CLI, the Codex IDE extension, and the ChatGPT desktop app share Codex host configuration. Put the Token in the environment and add this to `~/.codex/config.toml` or a trusted project's `.codex/config.toml`:

```toml
[mcp_servers.gemcp-project]
url = "https://gemcp.example.com/mcp"
bearer_token_env_var = "GEMCP_AGENT_TOKEN"
```

```bash
export GEMCP_AGENT_TOKEN='gmc_abcd123_<secret>'
codex mcp list
```

In ChatGPT desktop, open **Settings**, select **MCP servers**, and restart after adding the Streamable HTTP server. Use `/mcp` in Codex or ChatGPT to inspect the connection. Codex configuration is TOML, so the exported JSON is a source for the URL and Token rather than a directly importable file.

## Other MCP clients

Any MCP client that supports Streamable HTTP and custom headers can connect with:

```json
{
  "type": "http",
  "url": "https://gemcp.example.com/mcp",
  "headers": {
    "Authorization": "Bearer <Agent Token>"
  }
}
```

The client must preserve the `Authorization` header on initialize, session, Tool, Resource, and Prompt requests. It must not redirect authenticated requests to another origin. A `401` response means the Token is missing, malformed, expired, revoked, or no longer valid for the project.

Gemcp sends a standard SSE comment immediately after a successful authenticated standalone stream opens. This ensures reverse proxies such as Cloudflare forward the stream before any server-initiated message exists. Clients that do not need server-initiated notifications may disable the optional standalone SSE stream and use Streamable HTTP POST responses only.

## Verify a new connection

Start with `get_usage_guide`, or read `gemcp://docs/agent-guide` when the client supports MCP Resources. This confirms authentication, the intended project ID, Token scopes, and the current operating contract without creating paid work.

Then call `get_project_options`. It is read-only and confirms all of the following:

- the HTTPS endpoint is reachable;
- the bearer header is present;
- the Token maps to the intended project;
- the Token has `read` scope;
- approved repository, environment, and resource-profile IDs are visible.

The installer also calls `get_project_cost` as a read-only setup check. Normal work then uses `prepare_experiment`, which resolves UUIDs, a moving ref, immutable commit, preflight, cost, and idempotency on the server. `get_project_options` and `get_project_cost` remain available for inspection and the Advanced direct path.

The production Agent handoff includes a complete initial instruction. Its central approval boundary is:

```text
Use Gemcp only through its MCP tools. Read the Gemcp usage guide when needed.
Use prepare_experiment for normal work. Present the exact immutable proposal and
worst-case reservation, then wait for human approval of its confirmation digest
before calling submit_prepared_experiment.
```

## Tools

- `get_usage_guide`: current Agent operating guide, authenticated project ID, Token scopes, Resource URI, and Prompt name.
- `prepare_experiment`: resolve a repository/ref, safe argv, compatible defaults, preflight checks, cost, and a short-lived immutable proposal without reserving budget.
- `submit_prepared_experiment`: submit one confirmed proposal by ID and digest; identical retries return the same Experiment.
- `get_project_options`: approved repositories, environments, resource profiles, and project limits.
- `submit_experiment`: Advanced compatibility path for a full commit SHA, arbitrary shell command, and caller-managed idempotency key.
- `get_experiment`: current state and immutable experiment specification.
- `list_experiments`: recent experiments with optional state filters.
- `cancel_experiment`: cancel queued work immediately or request cancellation of active work.
- `list_artifacts`: durable output path and registered artifact names.
- `get_project_cost`: current monthly reservations, estimated charges, adjustments, and available capacity.

Scope mapping:

| Scope | Required for |
| --- | --- |
| `read` | usage guide, options, experiment queries, artifact listing, and cost queries |
| `submit` | prepare, prepared submission, and Advanced direct submission |
| `cancel` | `cancel_experiment` |

Issue the minimum scopes needed by the third-party Agent.

## Prepared submission contract

The normal call may omit the repository and ref when the Project has one active repository with a default branch:

```json
{
  "argv": ["python", "tools/smoke.py"],
  "runtime_preset": "smoke"
}
```

Preparation returns the resolved full commit, resources, checks, exact argv, display command, runtime policy, CNY reservation, expiry, proposal ID, and confirmation digest. It creates no Experiment, Attempt, Provider resource, or budget entry. After the human confirms the exact proposal, submit only:

```json
{
  "proposal_id": "proposal-id",
  "confirmation_digest": "sha256:exact-confirmed-digest"
}
```

Gemcp rechecks drift and budget. A retry with the same proposal returns the original Experiment.

## Advanced submission contract

`submit_experiment` requires:

```json
{
  "repository_id": "repository-uuid",
  "commit_sha": "full-40-or-64-character-commit-sha",
  "command": "python train.py",
  "max_runtime_seconds": 3600,
  "idempotency_key": "agent-run-unique-key"
}
```

`environment_id` and `resource_profile_id` may be omitted to select approved project defaults. IDs must come from `get_project_options`. `secret_names` must remain empty until project Secret storage and low-privilege Runner injection are implemented.

The idempotency key is scoped to the Agent Token. Repeating the same key and request returns the original experiment. Reusing the key with different arguments fails and never creates another reservation.

Gemcp reserves conservatively:

```text
ceil(price_to_milli * gpu_count * (max_runtime_seconds + timeout_extension_seconds + termination_grace_seconds + 600 + 30) / 3600)
```

Money values are milli-CNY. Submission fails when the reservation exceeds either the per-experiment cap or remaining monthly capacity.

## Execution boundary

Submission is durable even when dispatch is disabled. With `GEMCP_SCHEDULER_ENABLED=false`, accepted experiments remain `queued` and no paid Provider resource is created. Existing Attempts are still reconciled and settled. Enabling the flag activates FIFO dispatch for the validated Private Cloud account.

`cancel_experiment` immediately releases a queued reservation. For provisioning or active work, it records a durable stop request for the scheduler and independent Watchdog. Agents cannot select arbitrary Provider resources, retrieve Runner credentials, or operate raw machines.

## Rotation and incident response

1. Issue a replacement Token with the intended scopes and expiry.
2. Update the client's secret store and verify `get_project_options`.
3. Revoke the old Token from the Agents page.
4. Confirm the old client receives `401 Unauthorized`.
5. Review Agent Token prefixes and `last_used_at` for unexpected use.

Revocation does not abandon already-created experiments. The control plane and Watchdog retain lifecycle ownership.

Read [Execution and shutdown enforcement](execution.md) before enabling scheduling.

# Gemcp MCP Owner Guide

This guide explains how an Owner connects a third-party Agent to one Gemcp project and keeps paid execution under human control.

## What the Agent receives

An Agent Token is bound to one project and a selected set of scopes:

| Scope | Allows |
| --- | --- |
| `read` | Read project options, costs, experiments, artifacts, and the Agent guide |
| `submit` | Report controlled Agent phases and prepare or enqueue immutable experiments |
| `cancel` | Request cancellation of queued or active experiments |
| `configure` | Register and verify repositories, register AutoDL dataset bindings and HTTPS sources, register Provider-visible Environments, and declare dataset paths below an already approved trusted workspace root |
| `operate_nodes` | Register Cloud SSH hosts and rotate their credentials. Off by default. Not included in `configure`. |

The Token does not expose AutoDL credentials, repository deploy private keys, Runner Tokens, arbitrary machines, or arbitrary Provider operations. A `configure` Agent receives only the generated deploy public key so a repository administrator can install it read-only.

Existing active Token scopes can be edited from the Agents table. The update takes effect on the next authenticated request and is audited; no Token secret is displayed or rotated.

## Owner onboarding checklist

1. Sign in to Gemcp and select the intended project.
2. Confirm the repository is active and its exact commit is pushed.
3. Confirm the approved environment, GPU profile, budget, runtime, extension, grace, and concurrency policy.
4. Open **Agents**. For Cloud SSH training choose **Handshake prompt** (creates the setup link plus a prompt that covers MCP, host registration, and `prepare_experiment`). **MCP setup link** remains the enrollment-only path. Select minimum scopes and a finite credential lifetime, then create the short-lived link.
5. Send the one-time link only to the intended Agent. It works for Pi, Codex, OpenCode, Claude Code, and Grok. Do not open it through an untrusted link previewer or store it in a repository.
6. Let the Agent enable Gemcp MCP in the research repository directory. Its provisional credential is read-only until tool, guide, option, and cost verification succeeds.
7. Confirm the setup row becomes **Completed**, then let the Agent reload or restart its MCP client once so the Gemcp tools are available.
8. The advanced **Token** action remains available when you must install a secret through a client's secret store without the setup link.
9. Require the Agent to show the exact prepared Proposal, full commit, argv, resource, runtime, digest, expiry, and worst-case reservation before paid work.
10. Monitor the Study, iteration plan, hypotheses, and research Graph first. Each hypothesis lists linked Experiments, git branch, commit, and run records. Agent phases, Proposals, Experiments, runtime evidence, live output, and backend cleanup remain in the Lab layer. Agents monitor Experiments through `get_experiment` (`log_tail` and `metrics`); they do not receive SSH or remote files. Activity is limited to controlled phases and must never contain prompts, reasoning, source contents, environment values, or credentials. The Agent follows `get_next_actions`, binds a connected `from_node_id` into prepared Proposals, and closes runs with `close_run` (result plus highlight observation). Graph tools never start a workload. Vocabulary is the [Hypothesis–experiment Graph contract](../docs/graph-contract.md).
11. Revoke the issued Token immediately if its use is unexpected.

## Endpoint and authentication

```text
MCP endpoint: https://<gemcp-host>/mcp
Transport: MCP Streamable HTTP
Header: Authorization: Bearer <Agent Token>
```

The MCP setup flow writes this authentication configuration for the Agent that claimed the link. Enable it in the research repository directory. The advanced downloaded JSON embeds a live secret and should be imported only into the intended client.

## MCP setup links

An MCP setup link stores its 256-bit setup code only in the URL fragment. Browsers do not include the fragment in the setup-page request, access log, or Referer. Previewing the public setup page does not consume the link. The same link enrolls Pi, Codex, OpenCode, Claude Code, or Grok.

Claiming creates a short-lived `read`-only credential. The Agent stores that credential in a directory-local MCP config for its client. Pi may still use the fixed installer, which writes the Pi agent directory's `mcp.json` with mode `0600` and preserves other MCP servers. Every client must discover all twenty-eight Gemcp tools, call `get_usage_guide`, `get_project_options`, and `get_project_cost`, and then complete enrollment. Completion atomically applies the Owner-selected scopes and credential lifetime. The database stores only HMAC-SHA-256 digests of the setup code and Agent Token.

The setup link is shown once, may be claimed repeatedly only until completion for retry safety, and becomes unusable after completion, expiry, or revocation. The Owner can revoke pending or claimed setup from the console; revoking a claimed setup also revokes its provisional Token.

## Client configurations

### Claude Code

Project `.mcp.json`:

```json
{
  "mcpServers": {
    "gemcp-project": {
      "type": "http",
      "url": "https://<gemcp-host>/mcp",
      "headers": {
        "Authorization": "Bearer ${GEMCP_AGENT_TOKEN}"
      }
    }
  }
}
```

Launch Claude Code with `GEMCP_AGENT_TOKEN` in its environment, then use `/mcp` or `claude mcp list` to verify the server.

### Cursor

Project `.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "gemcp-project": {
      "type": "http",
      "url": "https://<gemcp-host>/mcp",
      "headers": {
        "Authorization": "Bearer ${env:GEMCP_AGENT_TOKEN}"
      }
    }
  }
}
```

Open Cursor MCP settings and verify the tools. Consult MCP Logs for HTTP errors.

### Visual Studio Code

Workspace `.vscode/mcp.json`:

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
      "url": "https://<gemcp-host>/mcp",
      "headers": {
        "Authorization": "Bearer ${input:gemcpAgentToken}"
      }
    }
  }
}
```

Run **MCP: List Servers**, start the server, and enter the Token through the password prompt.

### OpenAI Codex

User or trusted-project `config.toml`:

```toml
[mcp_servers.gemcp-project]
url = "https://<gemcp-host>/mcp"
bearer_token_env_var = "GEMCP_AGENT_TOKEN"
```

Set `GEMCP_AGENT_TOKEN` before launching Codex and verify with `codex mcp list` or `/mcp`. Prefer a trusted project's `.codex/config.toml` over a user-wide Codex host file.

### OpenCode

Project `opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "gemcp-project": {
      "type": "remote",
      "url": "https://<gemcp-host>/mcp",
      "enabled": true,
      "headers": {
        "Authorization": "Bearer ${GEMCP_AGENT_TOKEN}"
      }
    }
  }
}
```

Launch OpenCode with `GEMCP_AGENT_TOKEN` in its environment and confirm the Gemcp tools in the current repository only.

### Grok

Project `.grok/config.toml`:

```toml
[mcp_servers.gemcp-project]
url = "https://<gemcp-host>/mcp"
headers = { Authorization = "Bearer ${GEMCP_AGENT_TOKEN}" }
```

Set `GEMCP_AGENT_TOKEN` before launching Grok and verify with `grok mcp list` or `grok mcp doctor gemcp-project`. If doctor reports the folder as untrusted, run `grok --trust` in this directory (not `grok mcp doctor --trust`) and retry doctor. Prefer `--scope project` over a user-wide `~/.grok/config.toml`. Grok also loads project `.mcp.json`. Local HTTP uses `http://127.0.0.1:8080/mcp`.

### Other clients

The client must support MCP Streamable HTTP and preserve the `Authorization` header on initialize, session, tool, resource, and prompt requests. Never follow an authenticated redirect to another origin.

Gemcp primes an authenticated standalone SSE stream with a standard comment so reverse proxies forward it immediately. Clients that do not need server-initiated notifications may also disable the optional standalone SSE stream and use Streamable HTTP POST responses only.

## Agent handoff

Send the one-time MCP setup link, public project task, human approval policy, spend ceiling, and expected result contract. The link is itself a short-lived bearer capability; send it only in the intended private Agent session and do not repeat it after setup succeeds.

If you used the advanced Token path instead, configure the MCP connection with the secret Token through a separate secret channel, then give the Agent the non-secret `agent-mcp.md` guide and task. Do not paste a long-lived Agent Token into the Agent's natural-language prompt. The MCP client should inject it as an HTTP header.

Recommended instruction:

```text
Use the connected Gemcp MCP server and follow its Agent operating guide. Call
get_project_options and get_project_cost first. Before paid work, show me the
full commit SHA, exact command, approved environment/resource IDs, runtime,
worst-case reservation, and exact confirmation digest. Wait for my explicit
approval of that digest.
After submission, monitor the returned experiment ID to a terminal state and
report artifacts, metrics, estimated charge, and any failure details.
```

## Approval boundary

A `submit` scope is technical capability, not blanket financial approval. In v0.19, every prepared proposal requires per-experiment Owner confirmation of the exact digest after reviewing its immutable specification and worst-case reservation. The Owner may confirm in Evidence or explicitly authorize the Agent to submit that same digest. Gemcp enforces project budget and per-experiment caps, but those ceilings are not approvals. Standing approvals are not implemented; they remain a later design item.

Prepared submission has an idempotency boundary on the same proposal and digest. If the response is lost, retry that exact pair instead of preparing or approving a replacement run.

## Verification and troubleshooting

1. `get_usage_guide` succeeds: Token authentication and `read` scope work.
2. `get_project_options` returns the intended project, approved IDs, compute readiness, and the heartbeat contract. The Owner console **Agent Readiness** panel shows the same binding without probing.
3. `get_project_cost` returns current capacity in milli-CNY.
4. A 401 means the Token is missing, expired, revoked, malformed, or attached incorrectly.
5. Forbidden means the Token lacks the requested scope.
6. Commit verification failures mean the full SHA is not reachable from the registered repository.
7. Queued work should be queried, not resubmitted. The Owner should inspect runtime status and dispatch policy.
8. Cancellation is durable, but Provider cleanup is asynchronous; monitor until terminal state and resource release.

## Rotation and incident response

1. Generate a replacement Token with minimum scopes and an expiry.
2. Update the client's secret store.
3. Verify read-only tools with the replacement.
4. Revoke the old Token.
5. Confirm the old client receives 401.
6. Review Token prefixes, `last_used_at`, Experiments, costs, and managed Provider resources.

Revocation prevents new MCP requests but does not abandon already-created work. Gemcp and the independent Watchdog retain lifecycle ownership.

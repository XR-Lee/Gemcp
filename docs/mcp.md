# Third-party MCP clients

Gemcp exposes the official MCP Streamable HTTP transport at:

```text
https://<gemcp-host>/mcp
```

Every request requires a project-scoped Agent Token:

```http
Authorization: Bearer gmc_<identifier>_<secret>
```

Create a short-lived **MCP setup link** from the Owner console and let the Agent install its own credential. The same link works for Pi, Codex, OpenCode, Claude Code, and Grok. Enable Gemcp in the research repository directory, not as a global MCP. The advanced **Token** action remains available through the console or the [Agent access API](agent-tokens.md). Gemcp stores only HMAC-SHA-256 digests. Revocation and expiration are checked on every HTTP request, and the authenticated Token identity is bound to the MCP session.

## MCP setup link

The Owner sends one URL from `/agent/setup#code=...`. The Agent reads the public setup instructions at `/agent/setup` and enrolls its own MCP client. The code remains in the URL fragment and is not sent by link previews or ordinary page requests.

Claiming creates a short-lived `read`-only credential. After the Agent discovers all twenty-six tools and verifies guide, options, and cost, completion activates the Owner-selected scopes and lifetime. Claim and complete are retry-safe if the final response is lost.

## Pi with pi-mcp-adapter

Pi can run the fixed installer from the same configured origin when `pi-mcp-adapter` is already installed.

The installer merges a `gemcp-<project>` server into `<Pi agent dir>/mcp.json`, preserves existing servers, writes mode `0600`, exposes all twenty-six bounded Gemcp tools through `directTools`, and verifies tool discovery plus guide, options, and cost calls. A local credential-reading helper supports the current session without printing the Token. One `/reload` activates native `gemcp-<project>_*` tools through the adapter.

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

In ChatGPT desktop, open **Settings**, select **MCP servers**, and restart after adding the Streamable HTTP server. Use `/mcp` in Codex or ChatGPT to inspect the connection. Codex configuration is TOML, so the exported JSON is a source for the URL and Token rather than a directly importable file. Prefer a trusted project's `.codex/config.toml`.

## OpenCode

OpenCode reads project configuration from `opencode.json` or `opencode.jsonc`. Remote HTTP servers use `type: "remote"` under the top-level `mcp` key:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "gemcp-project": {
      "type": "remote",
      "url": "https://gemcp.example.com/mcp",
      "enabled": true,
      "headers": {
        "Authorization": "Bearer ${GEMCP_AGENT_TOKEN}"
      }
    }
  }
}
```

Set `GEMCP_AGENT_TOKEN` in the OpenCode process environment and keep the file in the research repository directory. Do not commit a live Token.

## Grok

Grok Build reads project `.grok/config.toml` first, then also loads `.mcp.json`, `.cursor/mcp.json`, and `~/.claude.json`. Prefer a project file so Gemcp is not a global MCP:

```toml
[mcp_servers.gemcp-project]
url = "https://gemcp.example.com/mcp"
headers = { Authorization = "Bearer ${GEMCP_AGENT_TOKEN}" }
```

```bash
export GEMCP_AGENT_TOKEN
grok mcp list
grok mcp doctor gemcp-project
```

`grok mcp add --scope project --transport http gemcp-project https://gemcp.example.com/mcp --header "Authorization: Bearer ${GEMCP_AGENT_TOKEN}"` writes the same project file. Grok expands `${GEMCP_AGENT_TOKEN}` at load time. Do not commit a live Token or put Gemcp in `~/.grok/config.toml` unless this machine exists only for this repository.

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
- authorized Self-hosted Nodes are discovered from current heartbeats even before a runtime is configured; fixed readiness codes explain missing runtime, workspace upgrade, availability, or capacity requirements without exposing Node credentials. An Owner-approved workspace path and successful image history are visible only to that Project Agent.
- experimental Cloud SSH nodes appear in `ssh_cloud_nodes` even before Project authorization. `get_project_options` probes them on the control plane. No SSH host key, password, or private key is ever returned.
- `readiness` summarizes ready vs blocked compute and repeats the heartbeat contract: MCP `last_used_at` updates on every authenticated call; Self-hosted `last_seen_at` is the node heartbeat; Cloud SSH `last_probed_at` updates on this probe or an Owner probe. Monitor runs with `get_experiment`. Agents are Project-scoped and not exclusively bound to one node.

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
- `list_repository_registrations`: active and pending repositories in the authenticated Project, including non-secret deploy public keys.
- `register_repository`: create a pending GitHub SSH registration in the authenticated Project; requires `configure`.
- `verify_repository`: activate a pending repository after its read-only Deploy Key is installed; requires `configure`.
- `list_workspace_datasets`: declared dataset paths below Owner-approved trusted workspace roots.
- `register_workspace_dataset`: declare one normalized relative dataset path without authorizing a new host root; requires `configure`.
- `remove_workspace_dataset`: disable one declaration without deleting host data; requires `configure`.
- `get_research_workspace`: return Studies, the selected iteration plan, the research Graph, and legal next actions without starting a workload.
- `update_research_workspace`: create or update a Study, replace the active plan, or record a Graph node; historical nodes should set `occurred_at` from the evidence committer date and optional `commit_sha`. Requires `submit` and never starts a workload.
- `get_next_actions`: return only Graph-legal next steps for the selected Study.
- `close_run`: write a result node on a terminal Experiment that already has a Graph run; omit `metric_name` to copy the prepared `expected_metric` from the Experiment; optionally attach the full Git commit containing a durable result manifest as `result_commit_sha`; requires `submit`.
- `prepare_experiment`: resolve a repository/ref, safe argv, compatible defaults, preflight checks, cost, and a short-lived immutable proposal without reserving budget. `runtime_preset` may be `smoke` (300s), `probe` (3600s), or `train` (up to the Project max runtime). AutoDL probe/train require a Project dataset binding under `/root/autodl-fs/`. When a Study exists, `from_node_id` must be a hypothesis or plan node and is bound into the confirmation digest. For experimental Cloud SSH, omit `image` and optionally omit repository; pass `argv` and optional `cwd`. The digest pins host, user, cwd, and argv. A trusted Self-hosted workspace still accepts a public name, tag, or digest. Do not invent SSH credentials or fall back to AutoDL.
- `list_dataset_bindings` / `register_dataset_binding` / `remove_dataset_binding`: Project AutoDL dataset roots and `GEMCP_DATASET_*` injection; write tools require `configure`.
- `register_ssh_cloud_node`: register a Cloud SSH host for the Token's Project; requires `operate_nodes`. Credentials are write-only. Probe only checks connectivity and pins the host key.
- `rotate_ssh_cloud_node_credential`: replace the encrypted SSH password or private key; requires `operate_nodes`.
- `submit_prepared_experiment`: submit one confirmed proposal by ID and digest; identical retries return the same Experiment and bind its Graph run node. Agents do not auto-submit. The Owner console can confirm the same digest and start the Experiment.
- `get_project_options`: approved repositories, environments, resource profiles, project limits, dynamically discovered authorized Self-hosted Node readiness, experimental Cloud SSH node readiness, and a `readiness` summary with the heartbeat contract. Cloud SSH listing does not require prior Project access; the control plane probes registered nodes.
- `submit_experiment`: Advanced compatibility path for a full commit SHA, arbitrary shell command, and caller-managed idempotency key.
- `get_experiment`: current state, immutable specification, bounded `log_tail`, and `metrics.json` projection. This is the monitoring surface; it does not expose SSH or remote files.
- `list_experiments`: recent experiments with optional state filters.
- `cancel_experiment`: cancel queued work immediately or request cancellation of active work.
- `list_artifacts`: durable output path and registered artifact names.
- `get_project_cost`: current monthly reservations, estimated charges, adjustments, and available capacity.

Scope mapping:

| Scope | Required for |
| --- | --- |
| `read` | usage guide, options, research workspace, next actions, experiment queries, artifact listing, and cost queries |
| `submit` | research updates, close_run, prepare, prepared submission, and Advanced direct submission |
| `cancel` | `cancel_experiment` |
| `configure` | register and verify Project repositories; register or disable trusted-workspace dataset paths and AutoDL dataset bindings |
| `operate_nodes` | register Cloud SSH hosts and rotate their credentials; off by default and not included in `configure` |

Issue the minimum scopes needed by the third-party Agent.

## Prepared submission contract

The normal call may omit the repository and ref when the Project has one active repository with a default branch:

```json
{
  "argv": ["python", "tools/smoke.py"],
  "runtime_preset": "smoke",
  "from_node_id": "hypothesis-or-plan-node-id"
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

Submission remains durable while dispatch is disabled. With `GEMCP_SCHEDULER_ENABLED=false`, `prepare_experiment` includes a scheduler warning but the proposal remains eligible. After exact-digest confirmation, whether submitted by the Agent or confirmed in the Owner console, both prepared and Advanced paths persist the Experiment as `queued` and create no compute resource. Set `GEMCP_SCHEDULER_ENABLED=true` and restart Gemcp to begin FIFO dispatch, then confirm a current scheduler heartbeat and the selected backend's readiness. Existing Attempts are still reconciled and settled while new dispatch is off.

`cancel_experiment` immediately releases a queued reservation. For provisioning or active work, it records a durable stop request for the scheduler and independent Watchdog. Agents cannot select arbitrary Provider resources, retrieve Runner credentials, or operate raw machines.

## Rotation and incident response

1. Issue a replacement Token with the intended scopes and expiry.
2. Update the client's secret store and verify `get_project_options`.
3. Revoke the old Token from the Agents page.
4. Confirm the old client receives `401 Unauthorized`.
5. Review Agent Token prefixes and `last_used_at` for unexpected use.

Revocation does not abandon already-created experiments. The control plane and Watchdog retain lifecycle ownership.

Read [Execution and shutdown enforcement](execution.md) before enabling scheduling.

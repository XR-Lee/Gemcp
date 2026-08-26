# Agent Token management

Gemcp Owners can enroll MCP clients or issue, inspect, and revoke project-scoped Agent Tokens after first-run setup. MCP setup links are the default workflow for Pi, Codex, OpenCode, Claude Code, and Grok; direct Token export remains the advanced path. All Owner management routes require an authenticated Owner Session and CSRF protection.

Long-lived Agent Token plaintext is never listed. PostgreSQL stores only HMAC-SHA-256 digests, prefixes, scopes, status, expiry, and usage timestamps. MCP setup also stores only a digest of its short-lived setup code; its retryable provisional Token is deterministically derived with the server master key and is never stored recoverably.

## List project tokens

```http
GET /api/v1/projects/<project-uuid>/agent-tokens
```

The response contains no secret:

```json
{
  "data": {
    "tokens": [
      {
        "id": "token-uuid",
        "project_id": "project-uuid",
        "label": "training-agent",
        "prefix": "gmc_abcd123",
        "scopes": ["read", "submit", "cancel"],
        "status": "active",
        "expires_at": "2026-10-15T00:00:00Z",
        "last_used_at": "2026-07-17T00:00:00Z",
        "created_at": "2026-07-16T00:00:00Z",
        "updated_at": "2026-07-17T00:00:00Z"
      }
    ],
    "mcp_url": "https://gemcp.example.com/mcp",
    "config_template": {
      "mcpServers": {
        "gemcp-project": {
          "type": "http",
          "url": "https://gemcp.example.com/mcp",
          "headers": {
            "Authorization": "Bearer ${GEMCP_AGENT_TOKEN}"
          }
        }
      }
    },
    "config_file_name": "gemcp-project-mcp.json"
  }
}
```

Token `status` is `active`, `expired`, or `revoked`. Expiry is evaluated at response time. Expired records remain visible for audit history but fail authentication. The endpoint returns the newest 200 Tokens and sets `truncated: true` when older history exists.

## Agent Readiness

```http
GET /api/v1/projects/<project-uuid>/agent-readiness
```

Owner-only. This is the same compute picture an Agent gets from `get_project_options`, plus Token bindings and next actions. It does **not** probe Cloud SSH hosts.

The response contains no secrets:

- `status`: `ready`, `blocked`, `waiting_agent`, or `waiting_compute`
- `agents`: scopes, `last_used_at` (MCP heartbeat), and Cloud SSH nodes that Agent registered
- `compute.ssh_cloud` / `compute.self_hosted`: readiness, blockers, `last_probed_at` / `last_seen_at`, and whether a Cloud SSH node is bound to this Project
- `heartbeats` and `instructions`: tell the Agent to inspect with `get_project_options` and monitor with `get_experiment`

Agents are Project-scoped. They are not exclusively bound to one node. `last_used_at` updates on every authenticated MCP call. Self-hosted `last_seen_at` is the `gemcp-node` heartbeat. Cloud SSH `last_probed_at` updates when `get_project_options` probes or the Owner probes.

The same response includes `enrollments`, containing only setup metadata such as label, requested scopes, status, expiry, claim/completion timestamps, and issued Token prefix. It never includes a setup code, setup URL, or Token secret. The newest 50 enrollments are returned, with `enrollments_truncated: true` when older history exists.

## Create an MCP setup link

```http
POST /api/v1/projects/<project-uuid>/agent-enrollments
Content-Type: application/json
X-CSRF-Token: <session-csrf-token>
```

```json
{
  "label": "research-agent",
  "scopes": ["read", "submit", "cancel"],
  "expires_in_days": 30,
  "never_expires": false,
  "setup_expires_in_minutes": 240
}
```

`read` is mandatory because setup verifies the guide, options, and cost tools. Setup validity defaults to 240 minutes and must be 5 to 1440 minutes; each project may have at most 20 unexpired pending or claimed links. The `201 Created` response returns `setup_url` exactly once. Its 256-bit code appears only after `#code=` in the URL fragment, so browsers do not include it in setup-page requests, access logs, or Referer headers.

The trusted installer uses these public endpoints:

```http
POST /api/v1/agent-enrollments/claim
POST /api/v1/agent-enrollments/complete
```

Installer downloads, enrollment requests, and MCP verification use `User-Agent: Gemcp-Pi-Setup/1` so reverse proxies can identify the machine client. This header is not authentication; setup code and Agent Bearer Token checks remain mandatory.

Claim is retryable until setup completes and creates a provisional Token limited to `read` and the setup deadline. After all twenty-six tools plus the guide, options, and cost checks pass, complete atomically applies the Owner-selected scopes and full credential lifetime. Completion is idempotent so a lost final HTTP response can be retried safely. Invalid, expired, revoked, or completed claims return the same `410 AGENT_SETUP_INVALID` response.

The Owner can revoke pending or claimed enrollment:

```http
DELETE /api/v1/projects/<project-uuid>/agent-enrollments/<enrollment-uuid>
X-CSRF-Token: <session-csrf-token>
```

Revoking a claimed enrollment also revokes its provisional Token. Completed enrollment is managed through its issued Agent Token.

## Issue a token directly

```http
POST /api/v1/projects/<project-uuid>/agent-tokens
Content-Type: application/json
X-CSRF-Token: <session-csrf-token>
```

```json
{
  "label": "training-agent",
  "scopes": ["read", "submit", "cancel"],
  "expires_in_days": 90,
  "never_expires": false
}
```

Rules:

- `label` is required and at most 120 characters.
- Allowed scopes are `read`, `submit`, `cancel`, `configure`, and `operate_nodes`; at least one is required. `configure` can register repositories in the Token's Project and dataset paths below an already Owner-approved trusted workspace root, but cannot authorize a new host root. `operate_nodes` can register Cloud SSH hosts and rotate their credentials. It is off by default and is not included in `configure`.
- `expires_in_days` must be between 1 and 3650. It defaults to 90 when omitted.
- Set `never_expires` to `true` and omit `expires_in_days` for a non-expiring credential.
- A project can have at most 50 unexpired active Tokens.
- Archived projects cannot issue new Tokens.
- `GEMCP_PUBLIC_URL` must be a credential-free HTTPS origin. The server never derives the export URL from the request Host header.

The `201 Created` response contains the plaintext secret and complete JSON export once:

```json
{
  "data": {
    "token": {
      "id": "token-uuid",
      "project_id": "project-uuid",
      "label": "training-agent",
      "prefix": "gmc_abcd123",
      "scopes": ["read", "submit", "cancel"],
      "status": "active",
      "expires_at": "2026-10-15T00:00:00Z",
      "created_at": "2026-07-17T00:00:00Z",
      "updated_at": "2026-07-17T00:00:00Z"
    },
    "agent_token": "gmc_abcd123_<one-time-secret>",
    "mcp_url": "https://gemcp.example.com/mcp",
    "mcp_config": {
      "mcpServers": {
        "gemcp-project": {
          "type": "http",
          "url": "https://gemcp.example.com/mcp",
          "headers": {
            "Authorization": "Bearer gmc_abcd123_<one-time-secret>"
          }
        }
      }
    },
    "config_file_name": "gemcp-project-mcp.json"
  }
}
```

The exported file contains a live bearer credential. Keep it outside source control, restrict it to the intended OS account, and delete it after importing into a client secret store. Prefer the environment-variable template for shared project configuration.

Token creation and its `agent_token.issued` audit event commit in one transaction. If the HTTP response is lost after commit, the secret cannot be replayed; revoke the visible prefix and issue another Token.

## Update active token scopes

An Owner may change an existing active Token's scopes without exposing or rotating its secret:

```http
PATCH /api/v1/projects/<project-uuid>/agent-tokens/<token-uuid>
Content-Type: application/json
X-CSRF-Token: <session-csrf-token>
```

```json
{"scopes":["read","submit","cancel","configure"]}
```

The change applies on the next authenticated request and writes an `agent_token.scopes_updated` audit event containing the previous and new scope sets, never the Token secret.

## Revoke a token

```http
DELETE /api/v1/projects/<project-uuid>/agent-tokens/<token-uuid>
X-CSRF-Token: <session-csrf-token>
```

Revocation is idempotent. New MCP requests fail immediately, and an authenticated MCP session cannot be reused under a revoked Token. Existing experiments and owned Provider resources remain under scheduler and Watchdog management.

Gemcp records `agent_token.revoked` once when status first changes. Owners may list and revoke credentials for archived projects even though those projects cannot issue new credentials.

## Console workflow

1. Sign in as Owner, select the project, and read **Agent Readiness** on Research. It shows usable Agents, bound compute, and heartbeats.
2. Use **Copy readiness** when an Agent is already connected: that prompt tells it to call `get_project_options` and lists the current binding.
3. For Cloud SSH training, open **Agents** and choose **Handshake prompt**. That creates the one-time MCP setup link and a prompt that tells the Agent to enable Gemcp in the repository directory, register the host when `operate_nodes` is granted, then `prepare_experiment`. **MCP setup link** remains the thinner enrollment-only path.
3. Send the handshake (or the link alone) only to the intended private Agent session. The setup URL is a bearer capability until completion and works for Pi, Codex, OpenCode, Claude Code, and Grok.
4. Let the Agent enable Gemcp MCP in the research repository directory; do not copy a Token into chat.
5. Confirm the enrollment becomes **Completed** and the Agent reports all four checks, then let it reload or restart its MCP client once.
6. Require the Agent to present the full commit, command, runtime, approved resource, idempotency key, and worst-case reservation before paid work.
7. Revoke the issued Token after rotation or any unexpected use and confirm the old client receives 401.

The advanced **Token** action remains available when a client must import a one-time JSON secret without using the setup link. Use the separate credential-free Agent handoff guide after that client is configured.

The running service also exposes the non-secret Owner and Agent guides at `/docs/owner-mcp.md` and `/docs/agent-mcp.md`. See [Third-party MCP clients](mcp.md) for the MCP Resource/Prompt discovery paths and client-specific configuration.

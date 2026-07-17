# Agent Token management

Gemcp Owners can issue, inspect, and revoke project-scoped Agent Tokens after first-run setup. All management routes require an authenticated Owner Session and CSRF protection.

Agent Token plaintext is returned exactly once. PostgreSQL stores only the HMAC-SHA-256 digest, prefix, scopes, status, expiry, and usage timestamps. A lost Token cannot be recovered; revoke it and issue a replacement.

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

`status` is `active`, `expired`, or `revoked`. Expiry is evaluated at response time. Expired records remain visible for audit history but fail authentication. The endpoint returns the newest 200 records and sets `truncated: true` when older history exists.

## Issue a token

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
- Allowed scopes are `read`, `submit`, and `cancel`; at least one is required.
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

## Revoke a token

```http
DELETE /api/v1/projects/<project-uuid>/agent-tokens/<token-uuid>
X-CSRF-Token: <session-csrf-token>
```

Revocation is idempotent. New MCP requests fail immediately, and an authenticated MCP session cannot be reused under a revoked Token. Existing experiments and owned Provider resources remain under scheduler and Watchdog management.

Gemcp records `agent_token.revoked` once when status first changes. Owners may list and revoke credentials for archived projects even though those projects cannot issue new credentials.

## Console workflow

1. Sign in as Owner, select the project, and open **Agents**.
2. Open **MCP guide** to review the approval boundary and supported discovery paths.
3. Choose **Generate token**, then select the minimum scopes and a finite expiry.
4. Copy the secret into a client secret store or download the one-time MCP JSON.
5. Download the separate **Agent handoff**. It contains no credential and can be given directly to the configured Agent.
6. Close the reveal dialog; Gemcp cannot display the secret again.
7. Verify `get_usage_guide`, `get_project_options`, and `get_project_cost` before permitting submission.
8. Require the Agent to present the full commit, command, runtime, approved resource, idempotency key, and worst-case reservation before paid work.
9. Revoke old credentials after rotation and confirm the old client receives 401.

The running service also exposes the non-secret Owner and Agent guides at `/docs/owner-mcp.md` and `/docs/agent-mcp.md`. See [Third-party MCP clients](mcp.md) for the MCP Resource/Prompt discovery paths and client-specific configuration.

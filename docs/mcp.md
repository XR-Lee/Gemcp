# MCP endpoint

Gemcp exposes the official MCP Streamable HTTP transport at:

```text
https://<gemcp-host>/mcp
```

Every request requires the project-scoped Agent Token returned by first-run setup:

```http
Authorization: Bearer gmc_<identifier>_<secret>
```

Store the Token in the Agent's secret configuration, not in source control or a command committed to shell history. Gemcp stores only an HMAC-SHA-256 digest. Revocation and expiration are checked on every HTTP request, and the authenticated Token identity is bound to the MCP session.

A generic remote MCP configuration has this shape:

```json
{
  "url": "https://gemcp.example.com/mcp",
  "headers": {
    "Authorization": "Bearer ${GEMCP_AGENT_TOKEN}"
  }
}
```

Use the environment-variable or secret interpolation syntax supported by the specific MCP client.

## Tools

- `get_project_options`: approved repositories, environments, resource profiles, and project limits.
- `submit_experiment`: verify a full Git commit SHA, reserve worst-case budget, and create an immutable queued experiment.
- `get_experiment`: current state and immutable experiment specification.
- `list_experiments`: recent experiments with optional state filters.
- `cancel_experiment`: cancel queued work immediately or request cancellation of active work.
- `list_artifacts`: durable output path and registered artifact names.
- `get_project_cost`: current monthly reservations, estimated charges, adjustments, and available capacity.

## Submission contract

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

`environment_id` and `resource_profile_id` may be omitted to select the approved project defaults. IDs must come from `get_project_options`.

The idempotency key is scoped to the Agent Token. Repeating the same key and request returns the original experiment. Reusing the key with different arguments fails and never creates another reservation.

Gemcp reserves conservatively:

```text
ceil(price_to_milli * gpu_count * (max_runtime_seconds + 600) / 3600)
```

The extra 600 seconds covers startup uncertainty. Money values are milli-CNY. Submission fails when the reservation exceeds either the per-experiment cap or remaining monthly capacity.

## Current execution boundary

In `v0.4.0`, accepted experiments remain durably `queued`. No AutoDL resource is created. Provider execution, Runner callbacks, Watchdog enforcement, and charge reconciliation are enabled only after live phase-zero validation selects the Elastic or Pro backend.

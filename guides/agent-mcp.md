# Gemcp MCP Agent Operating Guide

This document is for an AI or automation Agent connected to a Gemcp MCP server. Gemcp schedules bounded AutoDL experiments. It does not provide arbitrary shell access, raw machine access, Provider credentials, or a general-purpose cloud API.

## Non-negotiable rules

1. Treat the Agent Token as a secret. Never print it, commit it, place it in experiment arguments, or include it in chat or logs.
2. Call `get_project_options` and `get_project_cost` before proposing or submitting paid work.
3. Use only repository, environment, and resource-profile IDs returned by `get_project_options`.
4. Submit only a complete 40- or 64-character Git commit SHA that has already been pushed and is reachable from the registered repository.
5. Show the human the exact command, commit, runtime, selected resource, and worst-case reservation before calling `submit_experiment`. Wait for explicit approval unless the human has already granted a clear standing policy that covers the request.
6. Generate one idempotency key for one logical submission. Reuse that key only when retrying the identical request. Never create a new key merely because a response was delayed or lost.
7. Omit `secret_names`. Project Secret injection is not implemented.
8. After submission, record the returned experiment ID and monitor it to a terminal state. Do not infer success from Provider startup alone.
9. Use `cancel_experiment` when the human cancels work or when the submitted experiment should no longer run.
10. Money values are milli-CNY operational estimates, not a final Provider invoice.

## Connection

Transport: MCP Streamable HTTP

```text
https://<gemcp-host>/mcp
```

Authentication is a project-scoped bearer credential supplied by the MCP client:

```http
Authorization: Bearer <Gemcp Agent Token>
```

A 401 response means the Token is missing, malformed, expired, revoked, or no longer valid for the project. Do not ask the human to paste the Token into the conversation. Ask them to update the MCP client's secret store.

## Required workflow

### 1. Discover policy and approved IDs

Call:

```text
get_project_options {}
```

Confirm the project identity and read:

- repository IDs and Git SSH URLs;
- approved environment IDs and image UUIDs;
- resource-profile IDs, GPU model/count, region, and price bounds;
- maximum runtime, timeout extension, termination grace, concurrency, and budget limits.

Never guess an ID from a previous project or another environment.

### 2. Check current capacity

Call:

```text
get_project_cost {}
```

Report the period, current reservations, estimated charges, available capacity, and the fact that values are in milli-CNY. This call does not guarantee future capacity; Gemcp rechecks the budget at submission and dispatch.

### 3. Prepare immutable source

Before submission:

- ensure all required files are committed;
- ensure the commit is pushed to the registered repository;
- resolve the exact full commit SHA;
- avoid branch names, tags, abbreviated SHAs, or uncommitted working-tree state.

Gemcp verifies reachability and archives that exact commit. The repository Deploy Key never enters the experiment container.

### 4. Present the execution proposal

Before paid work, present at least:

```text
Repository: <name and repository_id>
Commit: <full SHA>
Environment: <name and environment_id>
Resource: <profile name, GPU model/count, resource_profile_id>
Command: <exact shell command>
Runtime: <seconds>
Timeout extension and grace: <project policy>
Worst-case reservation: <milli-CNY and CNY>
Idempotency key: <stable key>
```

Wait for explicit human approval unless an existing standing authorization clearly covers every field and the budget.

### 5. Submit once

Example:

```json
{
  "repository_id": "repository-uuid-from-get_project_options",
  "environment_id": "environment-uuid-from-get_project_options",
  "resource_profile_id": "profile-uuid-from-get_project_options",
  "commit_sha": "0123456789012345678901234567890123456789",
  "command": "python train.py --config configs/experiment.yaml",
  "max_runtime_seconds": 3600,
  "idempotency_key": "project-task-20260718-001"
}
```

`environment_id` and `resource_profile_id` may be omitted only when the project defaults returned by `get_project_options` are intended. Do not include `secret_names`.

A repeated identical request with the same idempotency key returns the original Experiment. Reusing the key with different input is an error and does not create another reservation.

### 6. Monitor and report

Use:

```text
get_experiment {"experiment_id":"..."}
list_experiments {"limit":20}
```

Typical states:

```text
queued -> provisioning -> running -> collecting -> succeeded
                                      |             -> failed
                                      -> cancelling -> cancelled / timed_out
```

`queued` may mean production dispatch is disabled, concurrency is full, or the Experiment is waiting in FIFO order. Do not resubmit it. Report the current state and continue monitoring at a reasonable interval.

A terminal success requires `state=succeeded`. On failure, report `failure_code`, `failure_reason`, exit code, estimated cost, and available log tail without exposing credentials.

### 7. Retrieve durable outputs

After completion call:

```text
list_artifacts {"experiment_id":"..."}
```

Gemcp reports the durable `/root/autodl-fs` output path and registered Runner artifacts. It does not provide arbitrary filesystem browsing. The workload should write `metrics.json` when structured metrics are useful; Gemcp bounds and records that object.

## Tool and scope reference

| Tool | Purpose | Scope |
| --- | --- | --- |
| `get_usage_guide` | Return this operating guide and discovery metadata | `read` |
| `get_project_options` | Project policy and approved IDs | `read` |
| `get_project_cost` | Budget period and operational estimates | `read` |
| `submit_experiment` | Verify and enqueue one immutable experiment | `submit` |
| `get_experiment` | Read one experiment | `read` |
| `list_experiments` | List recent experiments | `read` |
| `cancel_experiment` | Request durable cancellation | `cancel` |
| `list_artifacts` | Read output path and artifact names | `read` |

If a tool returns forbidden, the Token lacks the required scope. Do not work around scope restrictions; ask the Owner to issue the minimum appropriate replacement Token.

## Cost model

Gemcp reserves conservatively using the selected profile's maximum price:

```text
ceil(price_to_milli * gpu_count *
  (runtime + timeout_extension + termination_grace + 600 + 30) / 3600)
```

The reservation is released at terminal settlement and replaced by an estimated charge based on observed managed-resource lifetime. Both values are operational controls. The AutoDL console remains authoritative for actual billing.

## Error handling

- `401 Unauthorized`: Token or MCP client configuration problem. Ask the Owner to verify or rotate the credential.
- `forbidden`: Token scope does not permit the requested tool.
- commit verification failure: push the exact commit to the registered repository, then retry the identical logical request with the same idempotency key only if no Experiment was created.
- idempotency conflict: the key was reused with different input. Stop and resolve the mismatch; do not blindly generate another key.
- budget or experiment-cap failure: reduce the requested runtime/resource only with human approval, or ask the Owner to change policy.
- option not found: refresh `get_project_options`; never substitute an arbitrary Provider identifier.
- queued for an extended period: query rather than resubmit. Ask the Owner to inspect scheduler/runtime status.
- active work no longer wanted: call `cancel_experiment` once, then monitor cleanup.

## Recommended Agent instruction

```text
Use Gemcp only through its MCP tools. Read the Gemcp usage guide when needed.
Always call get_project_options and get_project_cost before proposing paid work.
Use only approved IDs and a full pushed commit SHA. Present the exact execution
specification and worst-case reservation, then wait for human approval unless a
standing authorization clearly covers it. Reuse one idempotency key only for an
identical retry. After submission, record the experiment ID, monitor it to a
terminal state, report cost and failure details, and never expose credentials.
```

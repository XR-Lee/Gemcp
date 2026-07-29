# Gemcp MCP Agent Operating Guide

This document is for an AI or automation Agent connected to a Gemcp MCP server. Gemcp schedules bounded AutoDL and authorized Self-hosted experiments. It does not provide arbitrary host access, Provider credentials, SSH, or a general-purpose cloud API.

## Non-negotiable rules

1. Treat the Agent Token as a secret. Never print it, commit it, place it in experiment arguments, or include it in chat or logs.
2. Report controlled workflow transitions with `report_agent_activity`. Never send prompts, private reasoning, arbitrary free text, environment variables, credentials, or source contents as activity.
3. Use `prepare_experiment` as the normal path. Let Gemcp resolve the repository, moving ref, full commit SHA, compatible defaults, preflight checks, cost, and idempotency.
4. Submit normal workloads as an ordered `argv`. Do not wrap it in a shell, add output-path wrappers, or interpolate typed values into shell text.
5. Show the human the returned repository, full commit, argv, runtime, backend resource, checks, expiry, and worst-case reservation.
6. Wait for explicit human approval of the exact confirmation digest before calling `submit_prepared_experiment`.
7. Submit a prepared proposal using only its proposal ID and exact digest. Never alter fields between preparation and submission.
8. A proposal retry uses the same proposal ID and digest and returns the same Experiment. A failed paid Experiment is never automatically resubmitted.
9. After submission, record the Experiment ID and monitor it to a terminal state. Do not infer success from Provider or Node startup alone.
10. Use `cancel_experiment` when the human cancels work or when the submitted Experiment should no longer run.
11. Use `submit_experiment` only when the human explicitly requests the Advanced shell-command compatibility path.

## Connection

Transport: MCP Streamable HTTP

```text
https://<gemcp-host>/mcp
```

Authentication is a Project-scoped bearer credential supplied by the MCP client:

```http
Authorization: Bearer <Gemcp Agent Token>
```

A 401 response means the Token is missing, malformed, expired, revoked, or no longer valid for the Project. Do not ask the human to paste the Token into the conversation. Ask them to update the MCP client's secret store.

## Required workflow

### 1. Inspect the local repository

When operating inside the user's repository, determine the intended program and arguments from reviewed source and configuration. Prefer an existing smoke script or documented entry point. Do not invent training flags, search for datasets outside the declared root, or add automatic downloads.

Report `inspecting_repository` before inspection and `selecting_workload` while resolving the reviewed entry point. Include only the registered remote and ref when known.

The normal request can be as small as:

```text
Run the smoke test for this repository.
```

The tool call contains transport context that the user should not have to copy:

```json
{
  "repository_remote": "git@github.com:owner/repository.git",
  "ref": "main",
  "argv": ["python", "tools/smoke.py"],
  "runtime_preset": "smoke"
}
```

Omit `repository` and `repository_remote` when the authenticated Project has exactly one active repository. Omit `ref` to use its default branch. Omit Environment and Resource Profile selectors to use an unambiguous compatible default.

For Self-hosted inspection, `get_project_options.self_hosted_nodes` is generated from current Node heartbeats and Project authorization rather than runtime records. A discovered GPU can therefore appear before it is selectable. Treat `readiness=runtime_configuration_required` as an Owner configuration requirement: report the Node label and GPU model, ask the Owner to approve a digest-pinned image in the Nodes workspace, and do not invent an image or silently fall back to AutoDL. Other fixed blockers include `gpu_busy`, `node_incompatible`, `node_not_online`, `node_stale`, `argv_upgrade_required`, and `node_busy`. Physical Node selection remains server-owned.

### 2. Prepare at zero cost

Call:

```text
prepare_experiment
```

Preparation creates no Experiment, Attempt, Provider resource, or budget reservation. Gemcp:

- resolves the selected ref to a verified full commit SHA;
- validates and archives that exact commit;
- resolves compatible Environment and Resource Profile defaults;
- checks Scheduler, Watchdog, callback, concurrency, Provider or Node readiness, image, and budget;
- calculates the worst-case reservation;
- creates a short-lived immutable proposal and server-owned idempotency boundary.

Gemcp records `preparing_proposal` when preparation begins and `awaiting_confirmation` after the proposal is committed. Resolution or preflight blockers record `blocked`.

When preparation returns `choice_required`, present only the bounded candidates for the named field. Never guess between repositories or backend resources.

### 3. Present the immutable proposal

Present at least:

```text
Repository and requested ref
Resolved full commit SHA
Exact ordered argv and display command
Environment image and Resource Profile
Backend, GPU model and count
Runtime, timeout extension, and termination grace
Preflight failures and warnings
Worst-case reservation in CNY
Proposal expiry
Proposal ID and confirmation digest
```

Do not hide failed checks. A warning may be approved, but a blocked proposal cannot be submitted.

Wait for explicit human approval unless a future server-returned standing policy clearly authorizes this exact digest. The presence of `submit` scope is not financial approval.

### 4. Submit the prepared proposal once

Call:

```json
{
  "proposal_id": "proposal-id-from-prepare",
  "confirmation_digest": "sha256:exact-digest-shown-to-the-human"
}
```

Gemcp reruns drift-sensitive preflight checks and rechecks configuration and budget in the creation transaction. Configuration drift returns `experiment proposal changed after confirmation` and creates no Experiment. Prepare a replacement and obtain approval again.

A repeated identical call returns the original Experiment. Never create a replacement proposal merely because the submission response was delayed or lost.

### 5. Monitor and report

After submission, report `monitoring` with the returned Experiment ID. Gemcp also records the initial monitoring transition atomically with prepared submission.

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

`queued` may mean concurrency is full or the Experiment is waiting in FIFO order. Do not resubmit it. A terminal success requires `state=succeeded`. On failure, report the failure code and reason, exit code, Runner stage, source-download count, estimated cost, metrics, and available log tail without exposing credentials.

When interpreting a terminal result, report `reviewing_results` with that Experiment ID. Report `idle` after the review is complete. Monitoring and review reports are accepted only for an Experiment created by the same Agent Token.

### 6. Retrieve durable outputs

After completion call:

```text
list_artifacts {"experiment_id":"..."}
```

Gemcp reports the managed output reference and registered artifacts. It does not provide arbitrary filesystem browsing. Workloads should write structured results to `${GEMCP_OUTPUT_DIR}/metrics.json`; the execution environment supplies `GEMCP_OUTPUT_DIR` and the Agent must not replace it.

## Advanced compatibility path

`submit_experiment` preserves the original arbitrary shell-command interface for existing automation. It is not the normal user journey.

Before Advanced paid work:

1. Call `get_project_options` and `get_project_cost`.
2. Use IDs returned by the current Project, a complete pushed 40- or 64-character commit SHA, and an exact reviewed shell command.
3. Show the human the command, IDs, runtime, policy deadlines, caller-generated idempotency key, and worst-case reservation.
4. Wait for explicit human approval.
5. Reuse the same idempotency key only for an identical transport retry.

Do not translate a normal argv request into this path merely because it is familiar. Shell parsing and caller-managed idempotency are the compatibility behavior being retired from the primary interface.

## Controlled activity reporting

`report_agent_activity` accepts only these phases:

```text
inspecting_repository
selecting_workload
preparing_proposal
awaiting_confirmation
submitting
monitoring
reviewing_results
blocked
idle
```

The optional context is limited to repository remote, ref, and Experiment ID. `monitoring` and `reviewing_results` require an Experiment ID. Activity is an operational phase marker, not a transcript or general log channel. Never place user prompts, reasoning, code excerpts, filesystem searches, error prose, environment values, or secrets in its fields.

## Tool and scope reference

| Tool | Purpose | Scope |
| --- | --- | --- |
| `get_usage_guide` | Return this operating guide and discovery metadata | `read` |
| `report_agent_activity` | Report a controlled workflow phase without prompts or reasoning | `submit` |
| `prepare_experiment` | Resolve and persist a zero-cost immutable argv proposal | `submit` |
| `submit_prepared_experiment` | Submit one confirmed proposal, idempotently | `submit` |
| `get_project_options` | Inspect Project policy, approved IDs, and authorized Self-hosted Node readiness | `read` |
| `get_project_cost` | Inspect budget and accounting details for Advanced use | `read` |
| `submit_experiment` | Advanced direct shell-command submission | `submit` |
| `get_experiment` | Read one Experiment | `read` |
| `list_experiments` | List recent Experiments | `read` |
| `cancel_experiment` | Request durable cancellation | `cancel` |
| `list_artifacts` | Read managed output and artifact names | `read` |

If a tool returns forbidden, the Token lacks the required scope. Do not work around scope restrictions; ask the Owner to issue the minimum appropriate replacement Token.

## Cost model

Gemcp presents exact CNY strings in a prepared proposal and retains milli-CNY integers for API and audit compatibility. The Advanced reservation uses the selected profile's maximum price:

```text
ceil(price_to_milli * gpu_count *
  (runtime + timeout_extension + termination_grace + 600 + 30) / 3600)
```

The reservation is released at terminal settlement and replaced by an estimated charge based on observed managed-resource lifetime. These values are operational controls. The AutoDL console remains authoritative for actual billing.

## Error handling

- `401 Unauthorized`: verify or rotate the MCP credential without placing it in chat.
- `forbidden`: the Token lacks the required scope.
- `choice_required`: present the returned candidates and prepare again with the selected name or ID.
- commit or ref verification failure: push the intended source or correct repository access, then prepare again.
- proposal blocked: report failed checks; create no paid Experiment.
- proposal expired: prepare a replacement and obtain approval for its new digest.
- proposal changed: execution-relevant configuration drifted; prepare and confirm again.
- budget or Experiment-cap failure: reduce runtime or resources only with human approval, or ask the Owner to change policy.
- queued for an extended period: query rather than resubmit.
- active work no longer wanted: call `cancel_experiment` once and monitor cleanup.

## Recommended Agent instruction

```text
Use Gemcp's prepared path for normal work. Inspect the current repository and call
report_agent_activity at controlled workflow transitions without sending prompts,
reasoning, source text, environment values, or credentials. Call
prepare_experiment with an ordered argv and optional ref. Let Gemcp resolve IDs,
commit, resources, checks, cost, and idempotency. Show the exact returned proposal
and wait for explicit human approval of its digest. Then call
submit_prepared_experiment once, monitor the Experiment to a terminal state, and
report results and cleanup evidence. Use submit_experiment only for an explicitly
requested Advanced shell-command workflow. Never expose credentials.
```

# Lightweight experiment harness

Status: proposed deployment pattern; the result-commit handoff is implemented by `close_run.result_commit_sha`.

## Decision

Gemcp should support a lightweight experiment harness, but it must not be built into `gemcp-node` and it should not run as an active scheduler on every GPU node.

Run one harness per Project or Study. Prefer a small management host. Co-locate it with a Self-hosted GPU node only when that machine is the trusted, always-on research workstation. Even when co-located, use a separate Unix user or rootless container and a separate working tree.

The harness is a client of Gemcp, not a second execution backend:

```text
                         +-- read-only Git fetch/archive
                         |
Harness --outbound MCP--> Gemcp control plane --observe--> AutoDL Runner / gemcp-node / Cloud SSH
   |                              |
   |                              +-- bounded log_tail + metrics.json onto Experiment
   +-- isolated Git writer --> result branch / pull request
```

The harness never opens SSH, never receives a host key or password, and never reads files from a GPU machine. Monitoring is `get_experiment`. Result scalars come from the Experiment's projected `metrics.json`, not from log parsing. Gemcp remains authoritative for Studies, the active iteration plan, Graph legality, immutable proposals, approval, budget, scheduling, Experiment state, observation, and cleanup. Git remains authoritative for reviewed source and durable result manifests.

## Why it stays separate

`gemcp-node` holds the Node credential, is allowed to control Docker, and owns node-local workload state. Combining autonomous planning or Git push with that daemon would join three trust domains: node administration, experiment execution, and source publication.

Managed workload containers intentionally receive no Node Token, Agent Token, Git credential, or Docker socket. The harness must preserve that boundary. In particular, it must never mount:

- `/etc/gemcp-node`
- `/var/lib/gemcp-node`
- `/var/run/docker.sock`
- the Docker data root

Gemcp repository Deploy Keys remain read-only. Give the harness a distinct write credential, preferably a short-lived GitHub App installation token restricted to one repository. Do not make the Gemcp Deploy Key writable.

## Capability split

| Component | Allowed | Not allowed |
| --- | --- | --- |
| `gemcp-node` | Node sync, Docker lifecycle, bounded logs and metrics | Research planning, Git push, Agent MCP token |
| Workload container | Approved source, selected GPU, `/outputs`, declared datasets | Node or Agent credentials, Docker socket, Git push credential |
| Harness | Gemcp MCP, clean Git clone, result manifest publication, small local state | SSH, Docker control, direct GPU use, node storage, Provider credentials, log-derived metrics |
| Gemcp control plane | Scheduling, proposal and budget gates, Graph, SSH/Node/Runner observation, read-only Git verification | Publishing commits on behalf of the harness |

The normal harness Token should use `read` and `submit`. Add `cancel` only when automated cancellation is required. Remove `configure` after repository and dataset onboarding.

## Supported loop

1. Call `get_research_workspace` and `get_next_actions`.
2. Update the visible iteration plan with `update_research_workspace` when the scientific plan changes.
3. Commit and push reviewed source changes, then call `prepare_experiment` with that ref and the legal `from_node_id`.
4. Present the immutable proposal and wait for the required human confirmation of its digest.
5. Call `submit_prepared_experiment`, then poll `get_experiment` until terminal. That view already has `state`, `assessment`, `attempts`, `log_tail`, `metrics`, registered artifact names, and settlement. Do not SSH through Gemcp, do not fetch remote files, and do not analyze Docker logs to invent a scalar.
6. Build one result manifest from the terminal Experiment view. Copy metrics; do not infer or rewrite them silently. `close_run` may omit `metric_name` and copy the prepared `expected_metric` from that view.
7. Commit only that manifest from a clean publisher clone and push it to a dedicated result branch or pull request.
8. Call `close_run` with the result summary, approved scalar metric, and the full result commit as `result_commit_sha`.
9. Record the decision and replace the active iteration plan through `update_research_workspace`.

Example close:

```json
{
  "experiment_id": "4b3d8ca6-62e2-41bf-a380-d113efbe34ab",
  "title": "Ablation 7 result",
  "summary": "Validation accuracy improved without increasing peak memory.",
  "status": "succeeded",
  "metric_name": "validation_accuracy",
  "metric_value": 0.9142,
  "result_commit_sha": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
}
```

`result_commit_sha` must be a full 40- or 64-character hexadecimal SHA. Gemcp stores it on the result node. If it is omitted, the result node has no separate Git result reference; the source commit remains on the run node. The current MVP treats this value as an authenticated Agent assertion. Independent reachability and manifest verification belong in the Git-sync increment below.

`close_run` rejects a reported scalar that is absent from or differs from the terminal Experiment metrics. A succeeded result must use the prepared proposal's `expected_metric` when one was declared, and a non-succeeded Experiment cannot be relabeled as succeeded. General `update_research_workspace` calls cannot create or modify result nodes.

## Result manifest

Use a deterministic path so Gemcp and humans can find a result without accepting an arbitrary filesystem path:

```text
.gemcp/results/<experiment-id>.json
```

Recommended v1 payload:

```json
{
  "schema": "gemcp.result/v1",
  "project_id": "project-uuid",
  "study_id": "study-uuid",
  "run_node_id": "run-node-uuid",
  "experiment_id": "experiment-uuid",
  "source_commit_sha": "full-source-commit",
  "state": "succeeded",
  "finished_at": "2026-08-23T10:30:00Z",
  "metric": {
    "name": "validation_accuracy",
    "value": 0.9142
  },
  "summary": "Validation accuracy improved without increasing peak memory.",
  "artifacts": []
}
```

Keep large checkpoints and datasets out of normal Git history. The current `get_experiment` view is suitable for bounded metrics and log evidence; `list_artifacts` and `read_artifact` expose registered names and bounded control-plane reads, not a general artifact download channel.

Never run `git add -A` against an Owner-approved trusted workspace after a workload has modified it. Publish from a clean clone, write only the deterministic manifest path, reject symlinks, and inspect the exact diff before committing.

## Observation

Workloads write `${GEMCP_OUTPUT_DIR}/metrics.json`. The control plane copies a 64 KiB UTF-8 log tail and that bounded JSON object onto the Attempt and Experiment:

- AutoDL: Runner heartbeats and the finished callback
- Self-hosted: `gemcp-node` samples Docker logs and the managed output file
- Cloud SSH: the control plane polls the host process, a log tail, and optional remote `outputs/metrics.json` over held SSH, then deletes only `/var/tmp/gemcp/<assignment-id>/`

A living LLM conversation is not required for that projection. A harness (or any MCP client) only polls `get_experiment`. Graph `result` nodes stay an explicit `close_run` so a Git result commit and a scientific summary can still be attached; the control plane does not invent those claims from logs.

## Autonomy boundary

The harness may autonomously observe runs, update non-spending plans, prepare proposals, publish result manifests, close terminal runs, and propose the next action.

It may not currently submit a new spending or GPU proposal without the exact human confirmation required by `submit_prepared_experiment`. Dynamic planning does not imply dynamic spending authority. Full unattended loops require a separate Owner-approved automation lease that binds at least:

- repository and allowed command or named workload
- image and backend
- maximum runs, concurrency, runtime, and cumulative cost
- expiry and emergency-stop behavior
- expected metric and acceptable result schema

Until that policy exists, the useful automation level is "prepare, summarize, and wait for approval", not silent continuous experimentation.

## Failure and idempotency

The harness should keep a small durable journal keyed by Experiment ID with the source commit, run node, terminal state, result commit, and whether `close_run` and the plan update completed.

- A repeated Git publish uses the same manifest path and must not create a second result for the same terminal Experiment.
- `close_run` is idempotent after the produced result edge exists.
- A Git push that succeeds before the MCP call is retried from the recorded commit.
- An MCP close that succeeds before the plan update is followed by a plan-only retry.
- Multiple harness replicas require a Project/Study lease. The initial deployment should use one active replica.

## Git-only sync increment

A Git push alone does not currently notify Gemcp. The MVP deliberately keeps one explicit MCP projection after the push.

If the desired operator experience is truly "write Git only", add a `sync_research_commit` MCP operation or a signed GitHub webhook plus reconciler. Gemcp should use its existing read-only repository credential to fetch the full commit, read only the deterministic manifest, and atomically:

1. verify repository, Study, run, Experiment, source commit, terminal state, and schema;
2. compare the declared metric with the Experiment's recorded metrics and expected metric;
3. close the run and attach the result commit;
4. supersede the active plan from a bounded next-plan section;
5. write an idempotent sync receipt and audit event.

This increment belongs in the control plane. It does not require a new `gemcp-node` command or any Git credential on a GPU workload.

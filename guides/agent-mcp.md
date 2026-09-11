# Gemcp MCP Agent Operating Guide

This document is the operating contract for an AI or automation Agent **connected to a running Gemcp MCP server**. It is not the product README.

- Humans: [README.md](../README.md)
- Coding agents working in the Gemcp repository: [AGENTS.md](../AGENTS.md)

Gemcp schedules bounded AutoDL, authorized Self-hosted, and experimental Cloud SSH experiments. It does not provide arbitrary host access, Provider credentials, SSH material, or a general-purpose cloud API.

## Non-negotiable rules

1. Treat the Agent Token as a secret. Never print it, commit it, place it in experiment arguments, or include it in chat or logs.
2. Treat the research Graph as the execution contract ([Hypothesis–experiment Graph contract](../docs/graph-contract.md)). Call `get_next_actions` before spending. Keep the Owner-facing Study current with `get_research_workspace`. For a registered GitHub repository, extract raw experiment rows from distinct research branches into `record_experiment_catalog` (Setting, 方法, 实现, metric, 结果, link, hash) and confirm with `get_experiment_catalog`. That catalog is table-ready evidence, not the Graph, and never starts a workload. Never place prompts, private reasoning, credentials, or environment dumps in research text. Recording a Graph node never starts a workload.
3. Report controlled workflow transitions with `report_agent_activity`. Never send prompts, private reasoning, arbitrary free text, environment variables, credentials, or source contents as activity.
4. Use `prepare_experiment` as the normal execution path. Let Gemcp resolve the repository, moving ref, full commit SHA, compatible defaults, preflight checks, cost, and idempotency.
5. Submit normal workloads as an ordered `argv`. Do not wrap it in a shell, add output-path wrappers, or interpolate typed values into shell text.
6. Show the human the returned repository, full commit, argv, runtime, backend resource, checks, expiry, and worst-case reservation.
7. Wait for explicit human approval of the exact confirmation digest before calling `submit_prepared_experiment`.
8. Submit a prepared proposal using only its proposal ID and exact digest. Never alter fields between preparation and submission.
9. A proposal retry uses the same proposal ID and digest and returns the same Experiment. A failed paid Experiment is never automatically resubmitted.
10. After submission, Gemcp writes the Graph `run` node. Monitor with `get_experiment` (`state`, `log_tail`, `metrics`). Then call `close_run`; omit `metric_name` to copy the prepared `expected_metric` from that view. `close_run` also writes a highlight observation on the originating hypothesis. Do not invent a result while the Experiment is still running. Do not SSH, fetch remote files, or infer metrics from logs. Do not infer success from Provider or Node startup alone.
11. Use `cancel_experiment` when the human cancels work or when the submitted Experiment should no longer run.
12. Use `submit_experiment` only when the human explicitly requests the Advanced shell-command compatibility path and the Project has no active Study. An active Study requires `prepare_experiment` with `from_node_id`.

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

## Project configuration scope

The optional `configure` scope lets an Agent maintain bounded inputs inside its authenticated Project. It does not authorize paid execution, a new host workspace root, arbitrary mounts, Provider access, or cross-Project changes. `request_image_bake` writes a zero-cost requested bake; Owner digest confirmation in the Lab Images workspace is the only start of AutoDL Pro. There is no MCP tool that starts Pro.

To onboard a GitHub SSH repository:

1. Call `list_repository_registrations` and reuse an existing matching record when present.
2. Call `register_repository` with its name, `git@github.com:owner/repository.git` URL, and default branch. Gemcp returns a pending record and a read-only deploy public key.
3. Show the public key and repository to the human. Wait while a repository administrator adds it as a read-only GitHub Deploy Key. Never request a GitHub credential or claim the key was installed yourself.
4. Call `verify_repository`. Omit `host_key_fingerprint` only when Gemcp can reuse the Project's established GitHub host pin. A successful fetch changes the record to `active`.
5. Confirm the active repository appears in `get_project_options` before preparing work.

To declare existing data below a trusted workspace root, call `register_workspace_dataset` with a stable name and normalized relative path. Absolute paths, traversal, symlink escape, and a new host root are forbidden. The tool returns a container path and a fixed environment variable. For example:

```json
{
  "name": "scanobjectnn-objbg",
  "relative_path": "data/ScanObjectNN/main_split"
}
```

The resulting workload value is:

```text
GEMCP_DATASET_SCANOBJECTNN_OBJBG=/gemcp/workspace/data/ScanObjectNN/main_split
```

Use this environment variable in a reviewed repository script instead of searching the host or assuming a machine-specific absolute path. Registration does not copy, download, alter, or validate dataset contents at the control plane. `gemcp-node` confirms that the declared path exists and resolves inside the approved root before container creation. Dataset declarations are included in the Proposal digest; adding, removing, or changing one invalidates an earlier confirmation. `remove_workspace_dataset` disables only the declaration and never deletes host data.

Host Conda environments are not container environments and must not be registered as datasets. Select a public OCI image through trusted-workspace `prepare_experiment`, then keep dependency setup reproducible in that image or the verified repository.

## Public Elastic onboarding

Public Elastic does not mount a host disk and does not inject workspace datasets. Do not call `register_workspace_dataset` for AutoDL. Do not write `wget`, `curl`, conda, or a shell wrapper into `argv`.

1. Call `get_project_options` and follow `onboarding.public_cloud.next_steps`.
2. Call `register_dataset_binding` with `catalog=scanobjectnn-objbg` (or another `/root/autodl-fs/` root) and allowlisted HTTPS `sources`. This only declares the destination; it never uploads data.
3. Call `prepare_experiment` with `runtime_preset=provision` and omit `argv`. Confirm the digest. The Runner downloads those exact URLs onto `/root/autodl-fs`.
4. If the locked image lacks Python packages, call `register_environment` with a Provider-visible `image_uuid` from `provider_images`, or set `install_dependencies=true` so the Runner runs `python -m pip install --user -r requirements.gemcp.txt` from the verified commit. Do not compile mamba or change the image from inside the workload.
5. Then prepare `smoke`, `probe`, or `train` as usual. Probe and train still fail closed if the bound root or markers are missing.

Cloud SSH injects `GEMCP_DATASET_*` for already-present host paths. It does not download datasets.

## Research workspace

The Owner console starts from a Study, an iteration plan, and a research Graph. Infrastructure remains in a separate Lab layer. Call `get_next_actions` and `get_research_workspace` before preparing work. After a terminal Experiment, call `close_run` instead of free-form result nodes.

Registered GitHub repositories also have a bounded **experiment catalog**, separate from the 128-node Graph. Walk research branches (`autoresearch/*`, documented tags, result writeups, `EXPERIMENTS.md`, `experiment_graph.yaml`) and persist one row per distinct Setting with `record_experiment_catalog`. Copy numbers from the checkout; do not invent metrics. Each row must include Setting, 方法 (`method`), 实现 (`implementation`), metric, 结果 (`result`), link, and hash. `get_experiment_catalog` returns the same rows plus the repository registration (`ssh_url`, default branch, status, last verified) for the Owner panel. Catalog writes never start a workload and never create an Experiment, Proposal, or budget reservation.

A typical update is:

```json
{
  "study": {
    "name": "objbg-scan",
    "question": "Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy without extra GPU hours?"
  },
  "plan": {
    "goal": "Establish a reproducible OBJ-BG baseline.",
    "next_action": "Record the current smoke-run accuracy as the first Graph result."
  }
}
```

`prepare_experiment` requires `from_node_id` when the Project has an active Study. That ID must be a connected hypothesis, or a plan that traces back to a hypothesis through `leads_to` parents — isolated nodes and plans hanging only off the question cannot prepare — and is bound into the confirmation digest the Owner approves. `submit_prepared_experiment` re-validates that origin before committing budget and then writes the `run` node; if the Graph changes in that instant, the response carries the submitted Experiment plus `graph_bind_warning` — fix the Graph and retry the same submit instead of preparing again. `produced` edges are only legal from `run` to `result`, and only `close_run` may write that result plus the highlight observation on the hypothesis. Historical evidence uses `observation` nodes hung off a hypothesis with `leads_to`; do not leave observations unlinked. Set `occurred_at` from `git log -1 --format=%cI <sha>` and pass `commit_sha`; the Owner axis uses that evidence time, not the MCP write time. The Graph is still claim-based, not one node per commit. argv, image, GPU, logs, and cleanup stay in Experiment detail. Multiple Studies require an explicit `study_id`. `get_next_actions` proposes the next decision or Experiment from the hypothesis and its evidence.

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
  "runtime_preset": "smoke",
  "from_node_id": "hypothesis-or-plan-node-id",
  "expected_metric": "overall_accuracy"
}
```

Omit `repository` and `repository_remote` when the authenticated Project has exactly one active repository. Omit `ref` to use its default branch. Omit Environment and Resource Profile selectors to use an unambiguous compatible default. `runtime_preset` may be `smoke` (300s), `probe` (3600s), or `train` (up to the Project `max_runtime_seconds`). Probe and train on AutoDL require a registered dataset binding under `/root/autodl-fs/`.

For Self-hosted inspection, `get_project_options.self_hosted_nodes` is generated from current Node heartbeats and Project authorization rather than runtime records. A discovered GPU can therefore appear before it is selectable. Treat `readiness=runtime_configuration_required` as an Owner configuration requirement: report the Node label and GPU model and ask the Owner either to approve one trusted host workspace or to create an Advanced digest-pinned runtime. Do not invent a host path or silently fall back to AutoDL or Cloud SSH. Other fixed blockers include `gpu_busy`, `node_incompatible`, `node_not_online`, `node_stale`, `argv_upgrade_required`, `workspace_upgrade_required`, `dataset_upgrade_required`, and `node_busy`.

For Cloud SSH inspection, `get_project_options.ssh_cloud_nodes` is experimental. The control plane probes registered nodes during this call. Each entry includes `experimental: true` and a warning that the control plane holds host login credentials. Present that warning during confirmation. Do not ask the Owner to click Probe or Authorize. Do not invent an SSH host, key, or password, and do not fall back to AutoDL or Self-hosted when the selected backend is `ssh_cloud`.

`get_project_options.readiness` is the heartbeat contract. Your Token `last_used_at` updates on every authenticated MCP call; that is how the Owner sees you are alive. Self-hosted `last_seen_at` is the `gemcp-node` heartbeat. Cloud SSH `last_probed_at` updates when this tool probes or the Owner probes. You are Project-scoped, not exclusively bound to one node. After a run exists, monitor only with `get_experiment`. Do not SSH for logs or invent a private heartbeat.

The Owner handshake prompt is the intended handoff: it contains the MCP setup link, whether `operate_nodes` is granted, and the prepare/train contract. Prepare a Cloud SSH host yourself, then register it with `register_ssh_cloud_node` when the Token has `operate_nodes`. That scope is off by default and is not included in `configure`. Credentials are write-only. Probe only checks connectivity and pins the host key. On a CPU-only local box with `GEMCP_LOCAL_PROCESS_ENABLED`, `submit` is enough to register the loopback stub (`127.0.0.1` or omitted host), the host Environment, and catalog `modelnet40-mini`. Do not invent a remote SSH secret or an AutoDL image.

Call `prepare_experiment` with `argv` and optional absolute `cwd`. Omit `image`. Repository is optional. The confirmation digest pins host, user, cwd, and argv and warns that there is no container isolation. Do not ask the Owner to pick an image. After submit, monitor only with `get_experiment`. Do not SSH again for logs. Fixed blockers include `host_key_changed`, `node_not_active`, `runtime_configuration_required`, and `node_busy`. A GPU is optional.

When `execution_policy=trusted_workspace`, the Owner has intentionally exposed the returned `workspace_path` to this Project. Select that Environment and same-named Resource Profile together. You may pass an `image` such as `pytorch/pytorch:2.4.1-cuda12.1-cudnn9-runtime`; this parameter is rejected for every other Environment. Present the mutable-image warning and workspace path during confirmation. Inside the container use `${GEMCP_TRUSTED_WORKSPACE:?GEMCP_TRUSTED_WORKSPACE is required}` for shared code and data. A successful run records the resolved digest in `successful_images`, after which omitting `image` reuses the latest successful image. Physical Node selection otherwise remains server-owned.

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
Trusted workspace and registered dataset environment variables
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

`get_experiment` is the monitoring surface. It already includes the bounded log tail and `metrics.json` projection for AutoDL, Self-hosted, and Cloud SSH. Do not ask Gemcp to open SSH, download remote files, or analyze logs into a metric. Workloads write `${GEMCP_OUTPUT_DIR}/metrics.json`; Gemcp copies that object onto the Experiment.

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
| `list_repository_registrations` | List active and pending Project repositories and deploy public keys | `read` |
| `register_repository` | Create a pending GitHub SSH repository registration | `configure` |
| `verify_repository` | Verify access after the read-only Deploy Key is installed | `configure` |
| `list_workspace_datasets` | List declared dataset paths below approved workspace roots | `read` |
| `register_workspace_dataset` | Declare a normalized relative dataset path | `configure` |
| `remove_workspace_dataset` | Disable a dataset declaration without deleting data | `configure` |
| `list_dataset_bindings` | List AutoDL dataset bindings for Public Elastic and Private Cloud | `read` |
| `register_dataset_binding` | Register a `/root/autodl-fs/` or approved Cloud SSH root, optional catalog, and HTTPS sources | `configure` |
| `remove_dataset_binding` | Disable a dataset binding without deleting data | `configure` |
| `register_environment` | Register a Provider-visible AutoDL image as a Project Environment | `configure` |
| `remove_environment` | Disable a Project Environment | `configure` |
| `request_image_bake` | Request a zero-cost AutoDL Pro image bake; does not create a Pro instance | `configure` |
| `get_image_bake` | Get one image bake, including a finished `image_uuid` | `read` |
| `list_image_bakes` | List recent Project image bakes | `read` |
| `get_research_workspace` | Return Studies, the selected plan, Graph, hypothesis records, and next actions | `read` |
| `update_research_workspace` | Create or update a Study, plan, or Graph node without starting a workload | `submit` |
| `get_next_actions` | Propose the next decision or Experiment from the hypothesis and its evidence | `read` |
| `close_run` | Write a result and a highlight observation on a terminal Experiment that already has a run | `submit` |
| `get_experiment_catalog` | Return registered repository identity and extracted experiment catalog rows | `read` |
| `record_experiment_catalog` | Persist research-branch experiment rows (Setting, 方法, 实现, metric, 结果, link, hash) without starting a workload | `submit` |
| `report_agent_activity` | Report a controlled workflow phase without prompts or reasoning | `submit` |
| `prepare_experiment` | Resolve a zero-cost argv proposal; bind a connected `from_node_id` into the digest when a Study exists | `submit` |
| `submit_prepared_experiment` | Submit one confirmed proposal and bind its Graph run node | `submit` |
| `get_project_options` | Inspect Project policy, approved IDs, and authorized Self-hosted Node readiness | `read` |
| `get_project_cost` | Inspect budget and accounting details for Advanced use | `read` |
| `submit_experiment` | Advanced direct shell-command submission; rejected when a Study is active | `submit` |
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
- pending repository: show the returned Deploy Public Key and wait for a repository administrator to add it read-only before verification.
- repository verification failure: confirm the Deploy Key, repository URL, target ref, and established GitHub host pin; never ask for a GitHub credential.
- dataset registration failure: use a normalized relative path below the already approved trusted workspace; never substitute an absolute host path.
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
Keep the Owner research Graph current. Inspect the current repository, update the
Study and iteration plan, and call report_agent_activity at controlled workflow
transitions without sending prompts, reasoning, source text, environment values,
or credentials. Call prepare_experiment with an ordered argv and optional ref.
Let Gemcp resolve IDs, commit, resources, checks, cost, and idempotency. Show the
exact returned proposal and wait for explicit human approval of its digest. Then
call submit_prepared_experiment once, attach the Experiment to a Graph node,
monitor it to a terminal state, and report results and cleanup evidence. Use
submit_experiment only for an explicitly requested Advanced shell-command
workflow. Never expose credentials.
```

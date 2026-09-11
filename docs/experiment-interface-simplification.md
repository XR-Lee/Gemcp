# Experiment interface simplification

Status: implementation in progress

Scope: Agent MCP experiment preparation, paid confirmation, workload and dataset selection, result observation, and compatibility with the existing advanced submission path.

The first implementation slice now covers durable expiring proposals, sole-repository and compatible-default resolution, server-side ref resolution, one-shot argv, version-1 `gemcp.yaml` named workloads, zero-cost preflight, digest-confirmed idempotent submission, and shell-free argv execution in AutoDL and capability-compatible Self-hosted Nodes. Prepared presets now include `smoke`, `probe`, and `train` up to Owner-unfrozen Project runtime. AutoDL Dataset Bindings inject `GEMCP_DATASET_*` from `/root/autodl-fs/` roots. The Owner console can PATCH Project policy, register those bindings, prepare a proposal without an Agent Token, and confirm a prepared digest including Graph origin, dataset, workload, and repository access. After a succeeded one-shot, Evidence can save a reviewable `gemcp.yaml` draft as a Project workload without writing the source repository. Repository URL onboarding and public-repository readiness are on this tree: paste a GitHub HTTPS or SSH URL; public repositories skip the Deploy Key. Generic Experiment detail now includes normalized assessment, Attempts, and Runner stage history. Standing approvals remain a later slice.

## Summary

Gemcp's current Agent interface is safe but too close to its control-plane data model. A user who wants to run a workload must indirectly manage repository, Environment, and Resource Profile UUIDs, a full shell command, a runtime limit, a client-generated idempotency key, path conventions, and a conservative reservation calculation. An Agent can discover and fill these values, but the user must still understand the protocol well enough to instruct the Agent how to do so.

The preferred interface should accept workload intent, resolve control-plane details on the server, produce a zero-cost immutable proposal, and submit only that confirmed proposal. The proposal must preserve the existing full-commit, budget, approval, idempotency, lifecycle, and cleanup guarantees.

The Owner Diagnostics workflow already provides most of the correct safety model: preflight, a drift-protected proposal digest, explicit confirmation, server-side idempotency, complete Attempt observations, backend stop state, and cleanup-aware assessment. Generic Agent experiments should reuse that model instead of maintaining a separate lower-level user experience.

## Product correction: start from a repository

This proposal must start one step earlier than experiment preparation. A user does not arrive with a configured Repository ID, workload manifest, runtime preset, Environment, and Resource Profile. The common starting point is only:

```text
I have this Git repository. Run its smoke test.
```

There are two separate friction layers:

- repository onboarding, which grants Gemcp read access and establishes safe Project defaults once;
- experiment preparation, which should reuse those defaults on every run.

Adding prepared proposals without fixing onboarding would make submission safer but would not create a short path from a repository to a first result. The product should therefore expose a repository readiness workflow and a run workflow as one continuous experience.

### Repository-first golden path

For a single-repository Project, the intended path is:

```text
Owner: paste a GitHub repository URL
Gemcp: derive the name and host, create read-only access, and verify the default branch
Owner: perform the one unavoidable GitHub authorization step
Gemcp: inspect repository readiness and select approved Project defaults

User: "Run the smoke test for this repository"
Agent: prepare one zero-cost proposal
User: review a short summary and confirm
Gemcp: submit once, monitor, clean up, and return the result
```

The normal path must not ask the user to copy a Repository UUID, enter GitHub's host-key fingerprint, choose an Environment or Resource Profile on every run, calculate cost, create an idempotency key, provide a remote path, or assemble an output wrapper.

For GitHub repositories, Gemcp should own a release-pinned set of trusted GitHub SSH host keys rather than asking every Owner to transcribe the same public fingerprint for every repository. Private repository authorization initially remains a read-only Deploy Key step. A later GitHub App can reduce that remaining external action to repository selection and installation approval. Public repositories should require no Deploy Key.

### No-manifest first run

A reviewed `gemcp.yaml` is the best repeatable interface, but requiring one before the first experiment recreates the bootstrap problem. The first prepared-proposal release must also accept a one-shot argument vector suggested by the coding Agent after it inspects the local repository. Gemcp displays and binds that exact vector in the proposal. It does not silently infer or execute a command.

After a successful one-shot run, the Owner can save it as a Project workload or the Agent can add an equivalent `gemcp.yaml` to the repository. Subsequent requests use the named workload. Arbitrary shell text remains Advanced; the normal one-shot bridge uses an argument vector without shell interpolation.

## Current interface

The Agent currently performs this sequence:

```text
get_usage_guide
get_project_options
get_project_cost
present exact IDs, commit, command, runtime, reservation, and idempotency key
submit_experiment
poll get_experiment
call list_artifacts
call read_artifact
```

`submit_experiment` accepts:

```json
{
  "repository_id": "repository-uuid",
  "environment_id": "environment-uuid",
  "resource_profile_id": "resource-profile-uuid",
  "commit_sha": "full-commit-sha",
  "command": "arbitrary shell command",
  "max_runtime_seconds": 300,
  "idempotency_key": "caller-generated-key"
}
```

This contract is useful as an advanced primitive, but it should not be the primary user-facing abstraction.

## Observed usability problems

### Control-plane identifiers leak into user intent

Repository, Environment, and Resource Profile UUIDs are durable internal identifiers. They belong in an immutable proposal and audit detail, not in the normal human request. Most Projects have defaults or a small set of named options that the server can resolve unambiguously.

### The user or Agent constructs shell programs

Non-trivial experiments require multiline shell or Python commands, careful quoting, output-path handling, metrics serialization, and failure propagation. This makes confirmation verbose and gives an LLM unnecessary responsibility for infrastructure-sensitive command construction.

### Idempotency is delegated to the caller

The caller must invent a key, preserve it across transport retries, replace it when any logical input changes, and avoid reusing it after an Experiment has been created. A stored proposal already provides a natural server-managed idempotency boundary.

### Path policy exists mainly in prompts

The remote source root is dynamic, the dataset root is backend-specific, and durable output must use `GEMCP_OUTPUT_DIR`. Today these rules are expressed in prompts and shell wrappers. Prompt instructions are useful guidance but are not a policy object and cannot provide filesystem enforcement.

### Cost presentation exposes accounting internals

Agents receive milli-CNY fields and must calculate the worst-case reservation from the Resource Profile ceiling, runtime, timeout extension, grace period, and provisioning allowances. Internal credit adjustments also use ledger signs that are easy to misread. Humans should see normalized CNY summaries by default while exact milli-CNY values remain available for audit and APIs.

### Terminal observation is incomplete

The MCP Experiment view includes the latest Runner stage and metrics but does not include the Attempt log tail, Provider stop reason, backend cleanup state, or a complete artifact view. `AttemptView` and Diagnostics already expose some of these fields through Owner APIs. A terminal Agent currently needs multiple calls and still cannot obtain all lifecycle evidence from MCP.

### Diagnostics and normal experiments have different safety ergonomics

Diagnostics use a zero-cost preflight, immutable proposal digest, explicit confirmation, repeated drift checks, Project-scoped server-side idempotency, Attempt observations, backend observations, a timeline, and cleanup-aware assessment. Normal Agent experiments go directly from low-level input to paid submission.

## Design goals

- Make the first useful action start with a repository URL, not control-plane configuration objects.
- Reduce the single-repository repeat-run request to a workload name and optional typed parameter overrides.
- Let users describe a repository workload and typed parameters instead of a shell command and UUIDs.
- Preserve an exact full commit SHA and exact resolved execution specification before paid work.
- Make preparation zero-cost and side-effect free.
- Bind confirmation to all execution-relevant fields with a proposal digest.
- Move idempotency ownership to the server.
- Make code, dataset, and output boundaries explicit Project or repository policy.
- Return complete terminal evidence in one bounded Experiment detail response.
- Keep arbitrary commands available as an explicit Advanced interface.
- Preserve strict Project scoping, budget enforcement, cancellation, Watchdog cleanup, and auditability.

## Non-goals

- Do not weaken immutable-commit verification.
- Do not hide the exact resolved command from the confirmation view.
- Do not remove worst-case budget reservation.
- Do not automatically retry a failed paid Experiment.
- Do not turn Gemcp into an arbitrary Provider shell, SSH, or filesystem API.
- Do not treat prompts as a replacement for server-side policy.
- Do not require Dataset Snapshot support in the first proposal release.

## Desired user model

A first request should be close to:

```text
Run the smoke test for this repository.
```

When a repository exposes more than one workload, a normal request should be close to:

```text
Run PointMamba objbg-smoke from autoresearch/c1 with
config=c1c2_k2_h96, seed=2, and the smoke runtime preset.
```

The Agent should not need the user to provide UUIDs, an idempotency key, a code root, an output path, timeout extension, grace period, or a reservation formula.

The Agent may obtain the current Git remote and ref from its local workspace, but these are transport context, not human-facing input. Gemcp maps the normalized remote to an active Project repository and resolves the ref itself. If exactly one active repository exists, the repository field may be omitted. If a field is ambiguous, preparation returns a bounded choice list instead of requiring the user to call a separate discovery tool and copy an ID.

The resolved confirmation should still show:

- repository and full commit SHA;
- Environment image and Resource Profile;
- exact command or argument vector;
- Dataset Bindings and path checks;
- runtime and policy-derived deadlines;
- worst-case reservation;
- warnings and preflight failures;
- proposal ID and confirmation digest.

## Prepared Experiment API

### Prepare

Add a zero-cost MCP tool:

```json
prepare_experiment {
  "repository": "pointmamba",
  "ref": "autoresearch/c1",
  "workload": "objbg-smoke",
  "parameters": {
    "config": "c1c2_k2_h96",
    "seed": 2
  },
  "runtime_preset": "smoke"
}
```

The minimum repeat-run request may omit values that are unambiguous:

```json
prepare_experiment {
  "workload": "smoke"
}
```

The no-manifest bridge accepts a one-shot argument vector:

```json
prepare_experiment {
  "repository_remote": "git@github.com:owner/repository.git",
  "ref": "main",
  "argv": ["python", "tools/smoke.py"],
  "runtime_preset": "smoke"
}
```

Exactly one of `workload` or `argv` is accepted. `argv` is an ordered string array executed without parameter interpolation. Shell programs, redirections, pipelines, and multiline commands remain available only through the Advanced direct path.

Names may be accepted only when they resolve unambiguously inside the authenticated Project. UUIDs remain accepted for automation and Advanced use. Resolution follows these rules:

- omit `repository` when the Project has exactly one active repository;
- omit `ref` to use that repository's default branch at preparation time;
- omit Environment and Resource Profile to use compatible Project defaults;
- omit `runtime_preset` to use the workload default, or the built-in `smoke` preset for one-shot preparation;
- use `runtime_preset=provision` with no argv to download registered AutoDL HTTPS sources onto `/root/autodl-fs`;
- return structured `choice_required` candidates when any omitted field is ambiguous;
- never guess between multiple compatible options.

`ref` may be a branch, tag, or full commit at preparation time. Gemcp resolves it to a full commit SHA and stores only that SHA as the execution authority. Confirmation never authorizes a moving ref.

Preparation performs the relevant bounded checks:

- Project, repository, Environment, Resource Profile, workload, and Dataset Binding status;
- exact commit reachability and source archive safety;
- manifest schema and typed parameter validation;
- scheduler, callback, Watchdog, concurrency, and cleanup readiness;
- Provider capacity and image compatibility where available;
- current budget, per-Experiment cap, and reservation;
- command, working-directory, code-path, dataset-path, and output contract resolution.

Preparation creates no Experiment, Attempt, Provider resource, or budget entry.

The Agent should normally call this tool directly from the user's intent. Separate `get_project_options` and `get_project_cost` calls remain available for inspection and Advanced automation, but they are not mandatory steps in the normal human journey because preparation performs the authoritative checks and returns the resolved options and cost.

### Proposal response

```json
{
  "proposal_id": "exppr_...",
  "confirmation_digest": "sha256:...",
  "expires_at": "...",
  "eligible": true,
  "requires_confirmation": true,
  "checks": [],
  "proposal": {
    "repository_id": "...",
    "repository_name": "pointmamba",
    "commit_sha": "...",
    "workload": "objbg-smoke",
    "parameters": {
      "config": "c1c2_k2_h96",
      "seed": 2
    },
    "environment_id": "...",
    "environment_name": "torch-cuda11.8",
    "resource_profile_id": "...",
    "resource_profile_name": "rtx3090",
    "execution_mode": "argv",
    "argv": ["python", "tools/gemcp_pointmamba_run.py", "--config", "cfgs/c1c2_k2_h96.yaml", "--seed", "2"],
    "display_command": "python tools/gemcp_pointmamba_run.py --config cfgs/c1c2_k2_h96.yaml --seed 2",
    "runtime_seconds": 300,
    "reserved_cost_milli": 11475,
    "reserved_cost_cny": "11.475"
  }
}
```

The default proposal response should lead with a concise review object:

```text
PointMamba @ 4c92... / objbg-smoke
RTX 3090 x1 / smoke / 5 minutes
Maximum reservation CNY 11.475
Ready with 1 warning
```

UUIDs, host-key identity, image UUID, exact argument vector, deadlines, and milli-CNY fields remain in an expandable `execution_details` object. They remain digest-bound even when the human-facing summary is compact.

The digest should bind at least:

- Project and proposal identity;
- repository endpoint, pinned host key, and full commit SHA;
- manifest content and resolved workload parameters;
- Environment, image, backend, and Resource Profile bounds;
- Dataset Binding identities, versions, and canonical roots;
- exact command, argument vector, working directory, and output contract;
- runtime, timeout extension, termination grace, and provisioning bounds;
- price bounds and worst-case reservation;
- Secret bindings when that feature exists.

### Submit prepared proposal

Add:

```json
submit_prepared_experiment {
  "proposal_id": "exppr_...",
  "confirmation_digest": "sha256:..."
}
```

Submission reruns drift-sensitive checks immediately before the Serializable creation transaction. A changed repository, workload manifest, Dataset Binding, Environment, Resource Profile, policy, or reservation returns `EXPERIMENT_PROPOSAL_CHANGED` and creates no Experiment.

The invocation after explicit human confirmation is the confirmation signal. A separate caller-supplied boolean does not add meaningful protection for MCP.

### Server-managed idempotency

The stored proposal owns the submission fingerprint and idempotency digest:

- the first valid submission creates one Experiment;
- an identical retry returns the same Experiment;
- a changed digest or input is rejected;
- an expired proposal must be prepared again;
- an Experiment failure never causes automatic paid resubmission.

The existing caller-generated `idempotency_key` remains only on the Advanced direct submission tool for compatibility.

## Repository workload manifest

Repositories may define reviewed workloads in a versioned `gemcp.yaml` at the source root:

```yaml
version: 1

workloads:
  objbg-smoke:
    entrypoint:
      - python
      - tools/gemcp_pointmamba_run.py
    arguments:
      - flag: --config
        parameter: config
      - flag: --seed
        parameter: seed
      - flag: --epochs
        parameter: epochs
    runtime_preset: smoke
    datasets:
      - scanobjectnn-objbg
    parameters:
      config:
        type: config_path
        allowed_prefix: cfgs/
        required: true
      seed:
        type: integer
        minimum: 0
        default: 2
      epochs:
        type: integer
        minimum: 1
        maximum: 30
    outputs:
      metrics: metrics.json
      checkpoint_glob: checkpoints/*
```

Manifest requirements:

- the manifest is part of the immutable commit;
- paths are relative to the extracted source root and cannot escape it;
- normal workloads use an argument vector, not interpolated shell text;
- every parameter-to-argument mapping is explicit; the server never guesses flags or positional order;
- an omitted optional parameter omits its complete argument mapping, including the flag;
- parameter schemas reject unknown fields and unsafe path values;
- generated commands remain visible in the proposal;
- a repository can omit the manifest and use a proposal-bound one-shot `argv` or the Advanced interface;
- manifest parsing must be versioned, bounded, and independent of workload code execution.

For monorepositories, an optional relative `working_directory` may be allowed after path normalization. The default remains the extracted source root.

## Execution representation

The current Experiment, AutoDL Runner spec, Self-hosted Node protocol, and Docker launcher all carry one command string and execute it through `/bin/sh -lc`. A normal-path `argv` must not be implemented by quoting values back into that shell string while claiming shell-free semantics.

Add an immutable execution union:

```json
{
  "mode": "argv",
  "argv": ["python", "train.py", "--seed", "2"]
}
```

or, for compatibility only:

```json
{
  "mode": "shell",
  "command": "python train.py --seed 2"
}
```

The `argv` mode must be preserved as structured data in the proposal digest, Experiment snapshot, Runner spec, Self-hosted Command, audit detail, and result view. The AutoDL Runner launches it directly with `subprocess.Popen(argv, shell=False)`. A compatible `gemcp-node` passes the vector directly as the OCI process arguments instead of invoking `/bin/sh`. Nodes that do not advertise the argv protocol capability are ineligible for argv workloads. `display_command` is a human-readable rendering only and is never execution authority.

The existing `command` column and shell launch path remain for Advanced compatibility. New named workloads and one-shot normal proposals always use `argv`.

## Runtime presets

Project-scoped presets should combine policy that users should not repeatedly enter:

```text
smoke: runtime 300 seconds, default Environment, default one-GPU profile
probe: runtime 3600 seconds, approved training Environment and profile
train: runtime 10800 seconds, explicit confirmation and a higher spend cap
```

A preset may constrain workloads, backends, GPU count, runtime, reservation, concurrency, and Dataset Bindings. Users can request a shorter runtime within policy. Timeout extension, termination grace, and provisioning deadlines remain server-owned.

## Dataset Bindings

Add a Project-scoped Dataset Binding model so users and Agents refer to a stable name instead of an absolute backend path:

```yaml
name: scanobjectnn-objbg
backend: autodl_private
canonical_root: /root/autodl-fs/datasets/ScanObjectNN
read_only: true
required_markers:
  - main_split/training_objectdataset_augmentedrot_scale75.h5
```

The initial AutoDL implementation can provide policy and preflight without claiming a mount-level sandbox that AutoDL does not provide. It should:

- resolve and normalize the configured root;
- prohibit workload parameters from selecting another root;
- inject a stable dataset environment variable;
- validate required markers through an approved bounded probe when possible;
- record the Binding snapshot in the proposal and Experiment;
- stop rather than search or download a fallback dataset when validation fails.

Later Dataset Snapshots can add content identity, immutable versions, asset placement, and backend-specific read-only mounts.

## Code and output roots

Users should never provide the remote code root. The Runner already executes the command with the extracted source directory as its current working directory. Workloads receive an explicit code-root environment value derived from that directory if needed.

Durable output remains the Runner-injected `GEMCP_OUTPUT_DIR`. A workload manifest may declare outputs, but it cannot replace or escape that root. Required metrics and result summaries are written there through a small workload runtime helper rather than repeated prompt-generated shell code.

The local Agent worktree root remains outside Gemcp's remote execution boundary. Agent integrations should launch in an isolated worktree or receive a harness-level working directory. A prompt may reinforce that boundary, but host-level isolation is required when a soft instruction is insufficient.

## Complete Experiment detail

Extend `get_experiment` so one bounded response is sufficient to explain a terminal run. The detail should contain:

```text
immutable proposal and resolved specification
Experiment state and normalized assessment
all Attempts with failure, exit, log-tail, metric, and timing fields
latest Runner stage and bounded stage history
source-download count and controlled Runner error type
ProviderResource or NodeAssignment state
backend stop reason, last error, and cleanup timestamps
cleanup_complete
registered artifact metadata
reservation, release, estimated charge, and finalization state
bounded lifecycle timeline
```

The compact `list_experiments` response should remain small. Only the detail endpoint should expand Attempts, timeline, logs, backend observations, and artifacts.

Diagnostics already has reusable `AttemptObservation`, `BackendObservation`, `TimelineEvent`, and `Assessment` concepts. These should move into a shared execution-observation model rather than be reimplemented separately for Agent experiments.

## Artifact access

`list_artifacts` returns a durable output path, registered names, and a manifest with media type, size, checksum, and availability. `read_artifact` returns a bounded control-plane copy of a registered text or JSON filename. It rejects path parameters. Shared-storage-only files such as `gemcp-launch.log` stay listed and unread. Checkpoint and archive transfer remain out of scope.

This must not become filesystem browsing or an SSH substitute.

## Approval policies

Explicit confirmation remains the default for paid work. An optional Owner-created standing policy can reduce repetitive confirmation for tightly bounded workloads:

```text
repository=pointmamba
workload=objbg-smoke
resource_profile=rtx3090
runtime_seconds<=300
reservation_cny<=12
daily_submissions<=3
max_concurrency=1
```

Standing approval should be valid only for named manifest workloads and typed parameters. Arbitrary command submission should continue to require explicit per-proposal confirmation by default. Every policy match records the rule ID and resolved proposal digest in the audit log.

## Cost presentation

API compatibility requires retaining milli-CNY integer fields. Human-facing proposal and result views should additionally present exact CNY strings and normalized ledger categories:

```text
base budget
credit adjustments
debit adjustments
active reservations
settled estimated charges
available capacity
```

The interface should avoid requiring users to interpret negative internal adjustment signs. It should distinguish worst-case reservation, likely or observed price, final operational estimate, and authoritative Provider billing.

## Web console

The Owner Web console and Agent MCP should use the same proposal object.

The first screen for an empty Project should be repository readiness, not a generic operations dashboard. It should:

- accept a GitHub HTTPS or SSH URL and derive the display name;
- distinguish public access from a required private-repository Deploy Key;
- show the single required external GitHub action with a direct repository-settings link;
- verify access, detect the default branch, inspect `gemcp.yaml`, and report readiness in one continuation step;
- use release-pinned GitHub host identity internally instead of asking for a fingerprint field;
- identify missing default Environment, Resource Profile, Dataset Binding, or runtime policy as actionable readiness items.

The normal form should show:

- repository and ref;
- named workload;
- typed workload parameters;
- Dataset Bindings;
- runtime preset;
- preflight results and confirmation.

Environment, Resource Profile, UUIDs, exact command, policy-derived deadlines, and raw reservation fields remain visible in an expandable execution-details section. An Advanced mode exposes direct immutable-command submission without making it the default workflow.

When a Project has one ready repository and one compatible set of defaults, opening the run form should require no infrastructure selection. After a successful one-shot run, the result view should offer `Save as workload` and generate a reviewable manifest draft without silently modifying the source repository.

## Security invariants

The simplified interface must preserve these invariants:

- paid dispatch always uses a verified full commit SHA;
- proposal confirmation is bound to all execution-relevant state;
- preparation creates no budget reservation or Provider resource;
- submission revalidates drift and budget inside the creation boundary;
- one proposal creates at most one Experiment;
- a failed Experiment is not automatically resubmitted;
- Project authorization and Token scopes are unchanged;
- workloads never receive Agent, Provider, Deploy Key, or control-plane credentials;
- dataset and source paths are normalized and cannot escape configured roots;
- output remains confined to the managed output contract;
- cancellation and independent cleanup enforcement remain durable;
- arbitrary shell and artifact access remain explicit, bounded Advanced features.

## Compatibility plan

Keep the current tools during migration:

```text
get_usage_guide
get_project_options
get_project_cost
submit_experiment              # Advanced direct path
get_experiment
list_experiments
cancel_experiment
list_artifacts
read_artifact
```

Add:

```text
prepare_experiment
submit_prepared_experiment
```

Existing Agent Tokens and scopes can initially remain unchanged: `submit` prepares and submits proposals or uses the Advanced path, while `read` reads proposals and Experiments. Existing Experiments require no data migration beyond additive observation fields.

The Agent guide should make the prepared path normative and label direct submission Advanced. Older clients continue working until a separately announced deprecation policy exists.

## Delivery plan

### Phase 1a - prepared Agent path (delivered in v0.13.0)

- Add a durable, two-hour Experiment Proposal model.
- Add `prepare_experiment` and `submit_prepared_experiment` MCP tools.
- Add the structured execution union end to end; execute `argv` without `/bin/sh` in the AutoDL Runner and capability-compatible Self-hosted Nodes.
- Resolve a sole repository, its default ref, and compatible Project defaults without UUID input.
- Resolve refs server-side to a full commit SHA using the registered repository credential.
- Support a proposal-bound one-shot `argv` and a built-in `smoke` runtime preset before manifests exist.
- Reuse Diagnostics preflight, digest, drift, cost, and server-side idempotency behavior.
- Return a compact human review plus complete digest-bound execution details.

This phase is the first releasable usability improvement. For an already configured single-repository Project, it reduces the normal MCP path from discovery, accounting, manual idempotency, and direct submission to `prepare_experiment`, human confirmation, and `submit_prepared_experiment`.

The released vertical slice is Agent-facing. Owner-session Proposal attribution and the Evidence prepare form are on this tree.

### Phase 1b - Owner prepared path (on this tree)

- Add Owner-session attribution to the Proposal model without fabricating an Agent Token (`agent_token_id` is optional).
- Add the same prepare, compact review, explicit confirmation, and submission flow to the Owner Console Evidence page.
- Reuse the authoritative resolver, preflight, digest, drift, cost, and idempotency implementation from Phase 1a.

### Phase 2 - repository readiness

- Replace separate name, SSH URL, branch, and fingerprint entry with repository URL onboarding.
- Use Gemcp-managed, release-pinned GitHub host identity.
- Support public GitHub repositories without Deploy Keys.
- Detect the default branch and manifest after access verification.
- Present Deploy Key installation as the only external step for a private repository.
- Add a readiness result covering repository access and compatible Project defaults.
- Evaluate a GitHub App only after the simplified Deploy Key workflow is measured.

### Phase 3 - close the observation gap

- Extend Agent Experiment detail with Attempt log tails and metrics.
- Add backend state, stop reason, last error, cleanup timestamps, and `cleanup_complete`.
- Include registered artifacts and settlement fields in the detail response. `get_experiment` and the Owner Evidence dialog now return registered artifact names plus reservation / estimated charge / `budget_finalized_at`. `list_experiments` stays compact. Bounded artifact reads are in Phase 5.
- Reuse Diagnostics timeline and assessment logic where practical. `get_experiment` and Owner Get now return a normalized `assessment` (running / passed / failed / cancelled, plus classification, summary, recommendations, and `cleanup_complete`) together with every Attempt and bounded Runner stage history. `list_experiments` stays compact.
- Add terminal success, failure, cancellation, timeout, and cleanup-pending tests.

### Phase 4 - workloads, runtime presets, and minimal dataset bindings

- Define and validate `gemcp.yaml` version 1.
- Add typed parameters, explicit parameter-to-argument mappings, safe relative paths, generated argument vectors, and output declarations.
- Add Project runtime presets and default resolution.
- Add named Dataset Bindings and environment injection needed by the first real training workloads.
- Add a small language-neutral workload result contract for `metrics.json`.
- Allow an Owner to save a successful one-shot proposal as a Project workload. Evidence offers `Save as workload`, previews a version-1 `gemcp.yaml` draft, and stores that named workload on the Project. Later `prepare_experiment` calls resolve `gemcp.yaml` at the verified commit first; if that file is missing or has no matching name, Gemcp uses the saved Project workload. Saving does not modify git.

### Phase 5 - dataset snapshots and artifacts (on this tree)

- Add backend compatibility and required-marker preflight. Local loopback hosts probe marker files and fail closed without searching. AutoDL and remote SSH record the markers and let the Runner fail closed.
- Add immutable Dataset Binding snapshots to Experiment specifications. `get_experiment` returns the prepare-time snapshot after later Binding edits.
- Add artifact manifests and bounded registered-artifact reads (`list_artifacts` / `read_artifact` / Owner detail).
- Extend later to Dataset Snapshots and asset placement.

### Phase 6 - standing approvals and experiment sets

- Add bounded Owner standing-approval policies for named workloads.
- Add per-policy submission and spend counters.
- Add explicitly bounded multi-experiment sets only after single-proposal behavior is mature.
- Keep failed paid Experiments single-shot unless a new proposal is separately authorized.

## Acceptance criteria

The design is successful when:

- an Owner can take a private GitHub repository from URL to readiness with only the unavoidable GitHub read-access action;
- an already configured single-repository Project can prepare a smoke run without preliminary options, cost, UUID, or idempotency calls;
- a repository without `gemcp.yaml` can use a reviewed one-shot argument vector for its first run;
- a normal user can request a named workload without entering UUIDs, paths, shell code, cost formulas, or an idempotency key;
- Gemcp resolves a moving ref to a full SHA before confirmation;
- the user can review the exact resolved command, resource, runtime, dataset, and reservation;
- configuration drift creates no Experiment and requires a replacement proposal;
- a lost submission response can be retried without creating another Experiment;
- a terminal `get_experiment` response explains workload, assessment, Attempts, Runner stages, backend cleanup, artifacts, and cost without database access;
- Dataset Binding violations fail before training instead of triggering path searches or downloads;
- the Advanced direct submission path remains available and equally safe;
- no simplification weakens budget, approval, credential, cancellation, or cleanup controls.

## Open decisions

- Whether Project-saved workloads can graduate into a generated manifest without creating conflicting sources of truth.
- The normalized Git remote forms accepted when an Agent identifies its current repository.
- Manifest schema evolution and the initial typed-parameter set after `gemcp.yaml` version 1.
- The minimum enforceable AutoDL Dataset Binding semantics before Dataset Snapshots exist.
- Artifact transfer limits and backend storage availability guarantees.
- Whether standing approvals apply to Agent Tokens, Projects, workload versions, or a combination.
- Whether generic Experiment assessment should use the same classifications as Diagnostics or a smaller common vocabulary.

The current proposal lifetime is two hours and proposals are immutable. Refreshing means preparing a replacement proposal. Gemcp resolves repository refs directly and stores the resulting full SHA; an Agent-supplied SHA is useful context but is never the execution authority without server verification.

## Current implementation anchors

- [`internal/experiment/proposal_prepare.go`](../internal/experiment/proposal_prepare.go) resolves and persists zero-cost prepared proposals.
- [`internal/experiment/proposal_submit.go`](../internal/experiment/proposal_submit.go) confirms drift and atomically creates the Experiment and reservation.
- [`internal/mcpserver/server.go`](../internal/mcpserver/server.go) registers the twenty-six MCP tools, including both prepared-path tools.
- [`internal/diagnostic/types.go`](../internal/diagnostic/types.go) defines the richer proposal and observation model.
- [`docs/diagnostics.md`](diagnostics.md) documents the existing drift-protected two-stage confirmation workflow.
- [`docs/execution.md`](execution.md) documents immutable execution, Runner output, deadline enforcement, and cleanup.
- [`docs/architecture.md`](architecture.md) defines the Provider, storage, and credential boundaries that this proposal must preserve.

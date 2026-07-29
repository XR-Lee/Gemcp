# Roadmap

## Released

### v0.1.0 - service foundation

Go/Gin service, PostgreSQL connectivity, embedded Vue console, container deployment, CI, and architecture baseline.

### v0.2.0 - phase-zero probes

Typed AutoDL clients, bounded read-only probes, gated paid Job probe, lifecycle observation, and defensive cleanup.

### v0.3.0 - identity and durable configuration

Ent schema, first-run transaction, Owner Session and CSRF, Agent Token hashing, encrypted Provider credentials, and PostgreSQL migration coverage.

### v0.4.x - project and Agent control plane

Project budgets and policy, private GitHub repository verification, immutable experiment submission, reservations, idempotency, MCP tools, Owner APIs, and the responsive Web console.

### v0.5.0 - live Private Cloud operations

Credential-safe Provider resource views, bounded pagination, safe DTOs, deployment details and events, and validate-before-commit Token rotation.

### v0.6.0 - production execution and shutdown

- Opt-in PostgreSQL FIFO scheduling with project/global concurrency and dispatch-time budget checks.
- Immutable infrastructure Attempts and deterministic managed-resource ownership.
- Private source archives and scoped Runner callbacks without Deploy Key exposure.
- Create uncertainty reconciliation, bounded infrastructure retry, Provider state reconciliation, and idempotent stop/delete.
- Local Runner deadline, controlplane enforcement, and independently deployed Watchdog.
- Reservation migration across UTC billing periods and terminal estimated-charge settlement.
- Owner managed stop, phrase-confirmed emergency stop, and service heartbeat status.
- Encrypted SMTP settings and a durable at-least-once critical-notification outbox.

Dispatch defaults off so upgrades cannot launch historical queued work; reconciliation remains active.

### v0.6.1-v0.6.4 - deployment and Runner validation

PostgreSQL 18 bind-root hardening, real Private Cloud Runner diagnostics, Provider-safe quote-free bootstrap launch, and a successful controlled RTX 3090 CUDA experiment with confirmed cleanup and ledger settlement.

### v0.7.0 - third-party Agent access

Owner-managed project Agent Token issuance, bounded scopes, optional expiry, effective-status and last-use views, immediate revocation, one-time MCP JSON export, audit events, client-specific Claude Code, Cursor, VS Code, and Codex documentation, plus session-cached Provider snapshots with visible-page background refresh.

### v0.7.1 - Cloudflare-compatible Runner callbacks

The initial downloader and all Runner requests send the stable `Gemcp-Runner/1` User-Agent instead of Python's default `Python-urllib/*` signature, which the production Cloudflare zone rejects with error 1010.

### v0.8.0 - production MCP onboarding

The production host serves self-contained Owner and Agent operating guides, the Agents page provides a credential-safe onboarding and handoff workflow, and Agents can discover the same mandatory preflight and approval contract through `get_usage_guide`, `gemcp://docs/agent-guide`, or the `operate_gemcp` Prompt.

### v0.8.1 - reverse-proxy SSE compatibility

Authenticated standalone MCP SSE streams begin with a standard comment, ensuring Cloudflare and similar reverse proxies forward the successful stream before the first server-initiated message. POST-only Streamable HTTP remains supported.

### v0.9.0 - one-link Pi enrollment

Owners create a short-lived one-time link instead of manually transferring a long-lived Token or editing Pi JSON. The Pi Agent claims a read-only provisional credential, atomically merges a mode-`0600` global adapter config, discovers all bounded tools, verifies guide/options/cost, and only then activates the Owner-selected scopes and lifetime. Setup codes and Agent Tokens remain digest-only in PostgreSQL, and completion plus the local installer are retry-safe.

### v0.10.0 - Self-hosted NVIDIA nodes

- Feature-gated Linux NVIDIA node enrollment through outbound HTTPS, Owner pairing approval, Project authorization, revocable digest-only credentials, and hardware identity quarantine.
- Durable single-GPU Assignments, at-least-once Commands and Events, transactional result projection, bounded infrastructure retry, cancellation, Emergency Stop, and lost-node reconciliation.
- Digest-pinned OCI execution under a dedicated `gemcp-node` daemon with fixed Docker isolation, local deadline enforcement, external GPU-use detection, restart recovery, and complete node-local logs.
- Zero-CNY Self-hosted runtime profiles, Node-authenticated verified source archives, Assignment observability, and an Owner Nodes console for enrollment, authorization, runtime configuration, and workload history.

Build Sessions, Environment and Dataset Snapshots, cross-node asset placement, and full historical output transfer remain later increments.

### v0.10.1 - dependency security update

Upgrade `golang.org/x/text` to `v0.39.0` to resolve reachable vulnerability `GO-2026-5970` reported by `govulncheck`.

### v0.10.2 - Private Cloud system-image compatibility

Treat browser-session rejection from AutoDL Private Cloud's optional `/api/v2/image/list` endpoint as a truncated system-image category while continuing to validate and display the documented Developer API resources.

### v0.10.3 - self-contained Node Agent handoff

Render the hosted Node Setup page with the control plane's exact release and commit, repository checkout verification, host preflight, immutable build, installer, setup-link custody, pairing, and post-install reporting requirements.

### v0.10.4 - bilingual Node Setup

Provide equivalent Chinese and English Node Agent handoffs selected by an explicit `lang` query or browser language, with a language control in the one-time Setup Link dialog and strict query validation in the installer and node client.

### v0.10.5 - bilingual operations console

Add an always-available Chinese / English switch to the console, login, and first-run setup surfaces. Apply language changes immediately across operational views, persist the preference locally, and use locale-aware dates and accessible control labels.

### v0.11.0 - finance analytics and internal credits

Add an Owner-only Finance workspace with monthly and Project filtering, budget and charge summaries, daily trends, backend attribution, per-Project capacity, an immutable budget ledger, relevant audit history, and append-only idempotent credits and debits. Internal adjustments change Gemcp scheduling capacity without presenting themselves as AutoDL payment, refund, or account-balance operations.

### v0.11.1 - resilient Runner startup diagnostics

Extend the bounded AutoDL runtime-prerequisite wait to five minutes, retry only transient Bootstrap downloads before Runner execution begins, and write credential-free launch stages to `gemcp-launch.log` in the immutable Experiment output path. Keep the Provider command below its 4096-byte boundary and preserve redirect rejection and single-execution semantics.

### v0.11.2 - observable pre-execution recovery

Retry transiently interrupted source bodies from a clean temporary file and retry the idempotent first `started` callback within the remaining provisioning window. Persist a controlled Bootstrap-stage vocabulary in the immutable audit log and expose the latest stage through Agent and Owner Experiment details. Preserve non-retryable HTTP and redirect behavior, enforce the existing source-download cap, execute the workload at most once, and clean up immediately when Bootstrap reports a terminal pre-execution failure.

### v0.12.0 - backend diagnostics

Add an Owner-only Diagnostics workspace with fixed GPU-connectivity and PyTorch-CUDA suites for AutoDL and Self-hosted backends. Run diagnostics through normal immutable Experiments, Attempts, source delivery, callbacks, output collection, settlement, cancellation, and cleanup. Gate dispatch on bounded preflight, explicit paid confirmation, a drift-protected proposal digest, Project-scoped hashed idempotency, and cleanup-aware result assessment.

### v0.13.0 - repository-first prepared Agent experiments

Add durable 30-minute Experiment Proposals and the `prepare_experiment` and `submit_prepared_experiment` MCP tools. Resolve a sole repository, server-verified full commit, compatible Environment and Resource Profile, source safety, backend readiness, capacity, budget, runtime bounds, and worst-case reservation before confirmation. Use the Proposal as the immutable drift and idempotency boundary. Carry structured argv end to end and execute it directly in the AutoDL Runner or capability-compatible Self-hosted Nodes while retaining `submit_experiment` as the Advanced shell path.

### v0.13.1 - Git archive compatibility

Accept the narrowly validated PAX global commit header emitted by `git archive` in prepared source preflight and Self-hosted Node extraction. Continue rejecting arbitrary global metadata and unsupported archive entry types.

### v0.14.0 - runtime observability workspace

Add a bilingual Owner operations feed for controlled Agent phases and immutable prepared Proposals. Project validated working/output paths, GPU observations, bounded log tails, and metrics from AutoDL Runners and Self-hosted Nodes onto Attempts and Experiments. Distinguish immutable requests from observed runtime state, keep active visible views current with guarded polling, and require a durable post-removal Node event before presenting Self-hosted cleanup as complete. Serialize PostgreSQL schema migration with a session advisory lock during concurrent control-plane startup.

### v0.14.1 - enrolled Node upgrade handoff

Generate a bilingual, Node-specific coding-Agent upgrade handoff from the Owner console, bound to the control plane's exact release and full commit. Preserve enrollment identity, credentials, bbolt state, and managed storage while atomically replacing the Node binary. Refuse upgrades while managed containers remain and automatically restore the previous binary when the new systemd service does not stay active.

### v0.14.2 - automatic Self-hosted capacity discovery

Project authorized Node hardware and readiness into `get_project_options` directly from current heartbeats, even before an approved runtime exists. Return fixed blockers such as `runtime_configuration_required` rather than hiding discovered GPUs. Highlight missing runtime boundaries in the Nodes workspace and pre-fill hardware-derived configuration while requiring explicit Owner selection of a digest-pinned image.

### v0.15.0 - trusted Self-hosted workspaces

Allow an Owner to authorize one normalized host directory for one Project and Node with a single form field. Generate hardware bounds and runtime records from current inventory, accept public image tags only for that explicit policy, bind the path and image into prepared confirmation, mount only the approved directory, resolve the image to a digest before launch, and record successful digests for reuse. Keep strict digest-pinned execution as the default policy.

### v0.15.1 - Agent-managed Project inputs

Add an explicit `configure` scope for current-Project GitHub SSH repository registration and verification, plus bounded dataset declarations below an existing Owner-approved trusted workspace root. Expose stable container paths and controlled environment variables, bind active declarations into Proposal drift checks, require Node-side existence and symlink-containment validation, and keep host-root authorization Owner-only. Allow an Owner to update an existing active Token's scopes without exposing its secret.

## In progress

### Repository readiness and Owner prepared experiments

Add repository URL onboarding and public-repository readiness, expose the prepared proposal and confirmation flow to authenticated Owners, then add reviewed named workloads. The released Agent path already removes preliminary options, cost, UUID, full-SHA, reservation calculation, caller idempotency, and shell-command assembly from the common single-repository workflow.

## Further validation

The following items still require the target environment or newly authorized paid resources:

- `/root/autodl-fs` output persistence after deployment deletion and cache eviction.
- Command failure, OOM, timeout extension, cancellation, repeated cleanup, and controlplane restart recovery.
- Watchdog enforcement while the controlplane is stopped.
- Private Cloud permission, capacity, invalid-image, and ambiguous-create behavior.
- Operational estimates against the Provider console, including whether `in_cache` containers are billed.
- SMTP deliverability through the selected production relay.
- Concurrent scheduling, GPU binding, Docker and daemon restart recovery, external GPU occupancy, node loss, and retry behavior across two physical Self-hosted NVIDIA machines.
- The fixed diagnostic suites on a paid AutoDL deployment and each authorized physical Self-hosted Node.

## Later increments

- Environment and resource-profile administration beyond first-run defaults.
- Repository-key rotation UI.
- Project-scoped Secret registration, rotation, and low-privilege Runner injection.
- Artifact manifests and controlled downloads where shared-storage access permits.
- Optional off-host backups and restore drills.
- Provider-specific billing import if AutoDL exposes a reliable Developer API.
- More Provider backends only after the Private Cloud execution path is operationally stable.

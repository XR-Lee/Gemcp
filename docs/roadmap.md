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

## Further validation

The following items still require the target environment or newly authorized paid resources:

- `/root/autodl-fs` output persistence after deployment deletion and cache eviction.
- Command failure, OOM, timeout extension, cancellation, repeated cleanup, and controlplane restart recovery.
- Watchdog enforcement while the controlplane is stopped.
- Private Cloud permission, capacity, invalid-image, and ambiguous-create behavior.
- Operational estimates against the Provider console, including whether `in_cache` containers are billed.
- SMTP deliverability through the selected production relay.
- Concurrent scheduling, GPU binding, Docker and daemon restart recovery, external GPU occupancy, node loss, and retry behavior across two physical Self-hosted NVIDIA machines.

## Later increments

- Environment and resource-profile administration beyond first-run defaults.
- Repository-key rotation UI.
- Project-scoped Secret registration, rotation, and low-privilege Runner injection.
- Artifact manifests and controlled downloads where shared-storage access permits.
- Optional off-host backups and restore drills.
- Provider-specific billing import if AutoDL exposes a reliable Developer API.
- More Provider backends only after the Private Cloud execution path is operationally stable.

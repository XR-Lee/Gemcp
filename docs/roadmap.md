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

Owner-managed project Agent Token issuance, bounded scopes, optional expiry, effective-status and last-use views, immediate revocation, one-time MCP JSON export, audit events, and client-specific Claude Code, Cursor, VS Code, and Codex documentation.

## Further validation

The following items still require the target environment or newly authorized paid resources:

- `/root/autodl-fs` output persistence after deployment deletion and cache eviction.
- Command failure, OOM, timeout extension, cancellation, repeated cleanup, and controlplane restart recovery.
- Watchdog enforcement while the controlplane is stopped.
- Private Cloud permission, capacity, invalid-image, and ambiguous-create behavior.
- Operational estimates against the Provider console, including whether `in_cache` containers are billed.
- SMTP deliverability through the selected production relay.

## Later increments

- Environment and resource-profile administration beyond first-run defaults.
- Repository-key rotation UI.
- Project-scoped Secret registration, rotation, and low-privilege Runner injection.
- Artifact manifests and controlled downloads where shared-storage access permits.
- Optional off-host backups and restore drills.
- Provider-specific billing import if AutoDL exposes a reliable Developer API.
- More Provider backends only after the Private Cloud execution path is operationally stable.

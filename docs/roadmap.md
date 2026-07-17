# Roadmap

## v0.1.0: Application foundation (complete)

- Go/Gin service and PostgreSQL readiness.
- Embedded Vue operations shell.
- Multi-stage container build and localhost-only Compose binding.
- Versioned, testable release workflow.

## v0.2.0: Phase-zero AutoDL validation (implemented, live validation pending)

- Typed AutoDL developer API client.
- Read-only balance, image, inventory, deployment, container, and event probes.
- Explicit spend-gated minimal Job probe.
- Secret-free provider behavior report and acceptance checklist.

## v0.3.0: Durable control foundation (complete)

- Ent/PostgreSQL tenant, Owner, Provider, project, environment, profile, repository, Agent token, Session, and audit schemas.
- AES-256-GCM credential encryption and HMAC Token digests.
- Serializable first-run initialization and one-time Agent Token display.
- Owner password login, revocable database Sessions, strict cookies, and CSRF validation.
- CI PostgreSQL migration coverage.

## v0.4.0: Agent-facing experiment core (complete)

- Per-repository encrypted Ed25519 Deploy Keys, pinned GitHub host verification, and immutable commit validation.
- Immutable experiments and attempts, Token-scoped idempotency, serializable budget reservations, and a durable FIFO-ready queue.
- Official MCP Go SDK Streamable HTTP endpoint with options, submit, get, list, cancel, artifacts, and cost tools.
- Project-scoped Agent Token authentication with per-request revocation, expiration, and scope checks.
- Owner project and repository management APIs.

## v0.5.0: M0 execution and operations

- Elastic Job or Pro provider selected by live phase zero.
- Runner callbacks, fixed AutoDL file-storage output path, and cost estimates.
- Independent watchdog and critical SMTP notifications.
- First-run, login, experiment list, and detail Web UI.

## Trial additions

- Multiple projects and Agent tokens.
- Project Secrets and dataset registry.
- Warm-cache validation and image synchronization.
- Approval queue and richer audit/cost reconciliation.
- Optional off-host backup and artifact object storage.

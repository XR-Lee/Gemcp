# Roadmap

## v0.1.0: Application foundation (complete)

- Go/Gin service and PostgreSQL readiness.
- Embedded Vue operations shell.
- Multi-stage container build and localhost-only Compose binding.
- Versioned, testable release workflow.

## v0.2.0: Phase-zero AutoDL validation (complete for public read probes)

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

## v0.4.1: Owner operations console (complete)

- Embedded first-run setup and Owner login flows.
- Project budget and queue overview, experiment list, filters, and immutable detail view.
- Repository registration, Deploy public-key display, and pinned-host verification UI.
- Tenant-scoped Owner experiment and cost APIs.
- Desktop and mobile Playwright browser coverage.

## v0.4.2: AutoDL Private Cloud phase zero (complete)

- Separate Private Cloud API host, inventory shape, CUDA selector, and deployment request contract.
- Private and system-image visibility without calling the unsupported public wallet endpoint.
- Overflow-safe live-spend estimates and protected local Token/report paths.
- Successful cold and stopped-container-reuse Jobs with stop/delete cleanup and zero residual deployments.
- Recorded `finished_num` terminal semantics and disposable `in_cache` behavior.

## v0.5.0: M0 execution and operations

- Production AutoDL Private Cloud Job Provider selected by live phase zero.
- Runner callbacks, fixed AutoDL file-storage output path, and local cost estimates.
- Independent watchdog and critical SMTP notifications.
- Provider lifecycle, live status, emergency-stop, and notification views in the existing Web console.

## Trial additions

- Multiple projects and Agent tokens.
- Project Secrets and dataset registry.
- Warm-cache validation and image synchronization.
- Approval queue and richer audit/cost reconciliation.
- Optional off-host backup and artifact object storage.

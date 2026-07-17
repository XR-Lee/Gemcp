# Architecture

## Boundaries

Gemcp is a private single-organization service. The initial data model remains tenant-aware, but the first release has one owner and one AutoDL provider account.

The control plane owns:

- Human configuration and audit through Web.
- Agent authentication and experiment operations through MCP.
- Project policy, budget reservation, and cost estimates.
- Provider reconciliation and resource ownership.
- Independent timeout and shutdown enforcement.

AutoDL owns physical scheduling, container execution, provider billing, images, and shared file storage.

## Runtime topology

The production image contains one Go binary and the embedded Vue assets. The Web application selects first-run setup, Owner login, or the operations console from server state; it does not require a separate frontend runtime. The same image supports separate process modes:

```text
controlplane: HTTP API, Web, MCP, scheduler, reconciler, email
watchdog:     independent overdue-resource shutdown loop
postgres:     authoritative durable state
```

M0 may run the scheduler in the controlplane process. The watchdog remains a separate process so a stuck request path cannot disable shutdown enforcement.

## Durable coordination

PostgreSQL is authoritative for identity, configuration, experiments, attempts, reservations, provider resources, idempotency records, audit events, and leases. In-memory queues may wake workers but never own job state.

A submitted experiment is immutable. Submission verifies a full Git commit SHA before a serializable transaction creates the experiment, budget reservation, Token-scoped idempotency record, and audit event. Infrastructure retries create attempts under the same experiment. A manual rerun creates a new experiment.

The remote MCP endpoint uses the official Go SDK's Streamable HTTP transport. Agent Bearer Tokens are checked against PostgreSQL for each request, and MCP sessions are bound to the authenticated Token identity.

## Provider boundary

The production AutoDL adapter uses documented Developer APIs; browser automation is excluded. Live phase zero selected **AutoDL Private Cloud Job** as the M0 execution backend. Public Elastic and Pro remain diagnostic clients, not production scheduling fallbacks.

Private Cloud differs materially from public Elastic: it has a separate API host, no Developer wallet endpoint, a non-regional GPU inventory, one `cuda_v` selector, and Provider statuses where `finished_num=1` may coexist with `status=running`. The official console's read-only system-image endpoint is used only to enumerate valid base-image UUIDs during phase zero.

The `v0.5.0` observer decrypts the Provider credential only inside the server process and exposes normalized Owner-only GPU, image, deployment, container, cache, and event views. Token rotation validates the candidate against all required read endpoints before an atomic encrypted update and audit event. Container access fields are intentionally absent from the decoded model.

The future execution adapter is idempotent at the control-plane boundary and records every Provider request ID available in responses. The validated Private Cloud installation did not return request IDs, so ownership records, immutable local attempt IDs, and reconciliation queries remain mandatory.

## Storage boundary

M0 uses an existing validated path under `/root/autodl-fs`. Experiment output remains in project-scoped directories there. PostgreSQL stores bounded log tails, scalar metrics, manifests, and paths, not large artifacts.

Stopped-container reuse is an opportunistic cache. Correctness cannot depend on a cache hit.

## Security boundary

Agent Bearer Tokens identify project-scoped principals and are stored as keyed hashes. AutoDL, Git deploy-key, SMTP, and project Secret values are encrypted with a master key that is not stored in PostgreSQL. GitHub host keys are pinned by trusted SHA256 fingerprint before a repository can become active.

Owner Sessions use Secure, HttpOnly, SameSite=Strict cookies plus CSRF validation for state-changing requests. API and MCP responses are marked `no-store`, including the one-time setup response containing the first Agent Token. Provider responses expose only a credential-presence boolean; neither plaintext Token nor ciphertext has an API representation.

Experiments may access the public Internet. Any injected Secret must therefore be project-scoped, low privilege, and readily rotatable. Gemcp does not claim to sandbox arbitrary experiment code.

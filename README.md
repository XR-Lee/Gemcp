# Gemcp

Gemcp is a private, single-organization control plane for running bounded AutoDL experiments through MCP while keeping provider credentials, budgets, lifecycle enforcement, and audit data under human control.

## Current release

`v0.7.1` gives every bootstrap and Runner callback a stable `Gemcp-Runner/1` User-Agent so Cloudflare does not reject Python `urllib` with error 1010. `v0.7.0` adds Owner-managed Agent Token issuance, expiry, revocation, one-time MCP JSON export, client-specific integration guidance, and a session-cached Provider view with visible-page background refresh. `v0.6.4` uses a Provider-safe, quote-free Runner launch command that waits for AutoDL's Miniconda interpreter and streams an encoded downloader over standard input. `v0.6.3` wraps the Runner bootstrap in an explicit Provider-side `/bin/sh -lc` command. `v0.6.2` fixes the in-container Runner bootstrap downloader while preserving redirect rejection. `v0.6.1` hardens PostgreSQL 18 bind-root initialization and the documented `v0.5.x` upgrade path. `v0.6.0` adds opt-in production execution for AutoDL Private Cloud: a PostgreSQL-authoritative FIFO scheduler, immutable Attempts, server-side private-source archives, scoped Runner callbacks, managed-resource ownership, local and independent hard deadlines, idempotent cleanup, and an independently deployed Watchdog. Owner operations now include managed deployment stop, confirmed emergency shutdown, service heartbeats, encrypted SMTP settings, and a durable critical-notification outbox.

New dispatch remains disabled after upgrade until `GEMCP_SCHEDULER_ENABLED=true` is explicitly set with a reachable HTTPS `GEMCP_PUBLIC_URL`. Reconciliation of existing Attempts still runs while dispatch is off; historical queued experiments cannot start automatically.

## Architecture

```text
Agent --HTTPS MCP--> controlplane --official API--> AutoDL
                         |
Human --HTTPS Web--------+
                         |
                     PostgreSQL
                         |
                     watchdog
```

The Vue frontend is embedded in the Go release binary. Redis, Kubernetes, and a separate frontend runtime are not required.

## Local checks

Prerequisites: Go 1.26.5 and Node.js 22+.

```bash
npm --prefix frontend install
make test
make frontend-test
make build

# Once per browser-test environment:
npx --prefix frontend playwright install chromium
make frontend-e2e
```

The backend requires PostgreSQL when started:

```bash
cp .env.example .env
GEMCP_DATABASE_URL='postgres://gemcp:gemcp@127.0.0.1:5432/gemcp?sslmode=disable' ./bin/gemcp serve
```

Health endpoints:

```text
GET /healthz
GET /readyz
GET /api/v1/version
GET /api/v1/projects
GET|POST /api/v1/projects/:id/agent-tokens
DELETE /api/v1/projects/:id/agent-tokens/:tokenID
GET /api/v1/repositories
GET /api/v1/experiments
GET /api/v1/experiments/:id/attempts
GET /api/v1/projects/:id/cost
GET /api/v1/provider
POST /api/v1/provider/query
PUT /api/v1/provider
GET /api/v1/provider/deployments/:id
GET /api/v1/provider/managed-resources
POST /api/v1/provider/deployments/:id/stop
POST /api/v1/provider/emergency-stop
GET /api/v1/runtime/status
GET|PUT /api/v1/notifications/settings
GET /api/v1/notifications
POST /api/v1/notifications/test
GET /api/v1/runner/bootstrap
GET /api/v1/runner/spec
GET /api/v1/runner/source
POST /api/v1/runner/events
POST|GET|DELETE /mcp
```

Phase-zero commands:

```bash
./bin/gemcp phase0 read --backend pro
./bin/gemcp phase0 read --backend elastic --region westDC2
./bin/gemcp phase0 read --backend private
```

A live Job probe is separately gated by a backend-specific JSON specification, a conservative spend cap, and an exact confirmation phrase. Read [Phase-zero validation](docs/phase-zero.md) and the sanitized [Private Cloud validation](docs/private-cloud-validation.md) before using it.

Generate both required bootstrap credentials on the deployment host:

```bash
./bin/gemcp keygen
./bin/gemcp bootstrap-token
```

Store them only in the protected deployment `.env`. The first-run setup transaction and Session API are documented in [First-run setup](docs/setup-api.md).

After initialization, validate the live credential and inspect resources through [Private Cloud Provider operations](docs/provider-operations.md). Register the private repository using the [Owner Web console](docs/web-console.md) or [Private Git repository API](docs/repositories.md), issue credentials through [Agent Token management](docs/agent-tokens.md), then connect a third-party Agent using the [MCP client guide](docs/mcp.md). Before arming execution, follow [Execution and shutdown enforcement](docs/execution.md) and configure [SMTP notifications](docs/notifications.md).

## Deployment

Copy the environment template beside the Compose file and keep it outside Git:

```bash
cp .env.example deploy/.env
cd deploy
docker compose up -d --build
```

Bind the origin to localhost and publish it through the configured Cloudflare Tunnel. Do not expose port 8080 directly to the public Internet. Existing Compose deployments must follow the [PostgreSQL 18 volume upgrade](deploy/README.md#postgresql-18-volume-upgrade) before recreating the database container.

## Security status

No real AutoDL, SMTP, Git, Runner, or experiment secret belongs in this repository. Provider, SMTP, and Git private credentials are encrypted at rest. Runner Tokens are Attempt-scoped, stored as HMAC digests plus recoverable ciphertext only until execution finalizes, and never passed to the user command environment. Local phase-zero Token files and reports are Git-ignored and must be mode `0600` inside a mode `0700` directory.

See [Architecture](docs/architecture.md) and [Roadmap](docs/roadmap.md).

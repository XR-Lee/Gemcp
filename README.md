# Gemcp

Gemcp is a private, single-organization control plane for running bounded AutoDL experiments through MCP while keeping provider credentials, budgets, lifecycle enforcement, and audit data under human control.

## Current release

`v0.5.0` uses the encrypted real AutoDL Private Cloud credential to power an Owner-only live resource console. It visualizes GPU capacity, private and system images, deployments, active containers, released caches, and deployment events, and supports validate-before-commit Token rotation. Accepted experiments remain queued until the production scheduler, Runner, and Watchdog land in `v0.6.0`.

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
GET /api/v1/repositories
GET /api/v1/experiments
GET /api/v1/projects/:id/cost
GET /api/v1/provider
POST /api/v1/provider/query
PUT /api/v1/provider
GET /api/v1/provider/deployments/:id
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

After initialization, validate the live credential and inspect resources through [Private Cloud Provider operations](docs/provider-operations.md). Register the private repository using the [Owner Web console](docs/web-console.md) or [Private Git repository API](docs/repositories.md), then connect an Agent using the [MCP endpoint](docs/mcp.md).

## Deployment

Copy the environment template beside the Compose file and keep it outside Git:

```bash
cp .env.example deploy/.env
cd deploy
docker compose up -d --build
```

Bind the origin to localhost and publish it through the configured Cloudflare Tunnel. Do not expose port 8080 directly to the public Internet.

## Security status

No real AutoDL, SMTP, Git, or experiment secret belongs in this repository. Provider credentials are encrypted at rest and configured only through the control-plane setup flow. Local phase-zero Token files and reports are Git-ignored and must be mode `0600` inside a mode `0700` directory.

See [Architecture](docs/architecture.md) and [Roadmap](docs/roadmap.md).

# Gemcp

Gemcp is a private, single-organization control plane for running bounded AutoDL experiments through MCP while keeping provider credentials, budgets, lifecycle enforcement, and audit data under human control.

## Current release

`v0.2.0` adds a typed AutoDL developer API client and a spend-gated phase-zero probe. Production scheduling remains disabled until live behavior is recorded and accepted.

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
```

Phase-zero commands:

```bash
./bin/gemcp phase0 read --backend pro
./bin/gemcp phase0 read --backend elastic --region westDC2
```

A live Job probe is separately gated by a JSON specification, a conservative spend cap, and an exact confirmation phrase. Read [Phase-zero validation](docs/phase-zero.md) before using it.

## Deployment

Copy the environment template beside the Compose file and keep it outside Git:

```bash
cp .env.example deploy/.env
cd deploy
docker compose up -d --build
```

Bind the origin to localhost and publish it through the configured Cloudflare Tunnel. Do not expose port 8080 directly to the public Internet.

## Security status

No real AutoDL, SMTP, Git, or experiment secret belongs in this repository. Provider credentials will be encrypted at rest and configured only through the control plane setup flow.

See [Architecture](docs/architecture.md) and [Roadmap](docs/roadmap.md).

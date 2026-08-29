# Gemcp

Gemcp is a private research control plane. The Owner sees a Study, hypotheses, and a research Graph. CLI/MCP Agents propose the next Experiment on that Graph; Gemcp schedules the work, reserves budget, and writes the result back as evidence. Provider tokens, node credentials, and audit stay in a separate Lab layer.

A paid run starts only from a connected hypothesis (or a plan that traces back to one). The contract is `hypothesis` → `prepare_experiment` → Owner-confirmed digest → `submit_prepared_experiment` → `close_run`. Vocabulary: [Hypothesis–experiment Graph contract](docs/graph-contract.md).

Work can run on a local CPU process (no NVIDIA), Cloud SSH, Self-hosted Docker GPUs, or AutoDL. The three remote backends do not fall back to one another.

Current version string: [`VERSION`](VERSION) (`0.20.0`). Per-version notes: [Release notes](docs/releases.md). Testers: [tester brief](docs/tester-brief.md). Use `main`. Do not use the stale `Jiyao` branch.

## Local use (no GPU)

Prerequisites: a Go 1.21+ command (the `go.mod` pin is 1.26.6 and will download that toolchain), Node.js 22+, and PostgreSQL 16+ on `127.0.0.1:5432`. Debian/Ubuntu apt Go and PostgreSQL are enough to bootstrap; they are not the production Compose versions.

```bash
./scripts/bootstrap-local.sh
./scripts/dev-serve.sh
```

That writes repo-root `.env` from `deploy/env.local.example`, generates `GEMCP_MASTER_KEY` and `GEMCP_BOOTSTRAP_TOKEN`, creates the `gemcp` role/database, and builds `./bin/gemcp`. `serve` / `watchdog` read that `.env` (process environment still wins). Development defaults `GEMCP_AUTO_MIGRATE=true` and, when `GEMCP_ENV=development`, turns on the scheduler, Cloud SSH, and local process overlay.

Open `http://127.0.0.1:8080`. First visit is setup (Owner → Compute → Project). Skip AutoDL. Copy the one-time Agent Token; Gemcp will not show it again. Owner email/password come from `.env` (`GEMCP_DEV_OWNER_*`).

In a second terminal:

```bash
./scripts/local-http-smoke.sh
./scripts/local-cpu-loop.sh
```

Smoke checks `/healthz`, `/readyz`, `/api/v1/version`, `/api/v1/setup/status`, first-run `skip_provider` when needed, and `POST /mcp` initialize. The CPU loop seeds `examples/local-cpu/` to `$HOME/gemcp/datasets/modelnet40-mini`, registers loopback compute / host Environment / `modelnet40-mini` through MCP, records a hypothesis graph, waits for a scraped heartbeat, and prints `CLI_DECISION=success|failure|new_observation`.

Do **not** copy `.env.example` for this path (that file is the production Compose template). Do **not** start serve with only `GEMCP_DATABASE_URL` on the command line: that ignores `.env` and dies on the master-key placeholder, or comes up without migrations.

Point an MCP client at `http://127.0.0.1:8080/mcp` with the Agent Token. Call `get_usage_guide`, `get_research_workspace`, and `get_next_actions` before spending. Do not call `submit_prepared_experiment` without the Owner-confirmed digest.

## Tests

```bash
npm --prefix frontend install
make test
make frontend-test
make build

# Once per browser-test environment:
npx --prefix frontend playwright install chromium
make frontend-e2e
```

## Production deploy

```bash
./bin/gemcp keygen
./bin/gemcp bootstrap-token
cp .env.example deploy/.env
# write the generated credentials, keep GEMCP_ENV=production
cd deploy
docker compose up -d --build
```

Bind the origin to localhost and publish it through the configured Cloudflare Tunnel. Do not expose port 8080 to the public Internet. Existing Compose deployments must follow the [PostgreSQL 18 volume upgrade](deploy/README.md#postgresql-18-volume-upgrade) before recreating the database container.

Paid AutoDL dispatch needs a reachable HTTPS `GEMCP_PUBLIC_URL` and `GEMCP_SCHEDULER_ENABLED=true`. Cloud SSH stays off until `GEMCP_SSH_CLOUD_ENABLED=true`. First-run setup, Sessions, and HTTP surfaces: [setup API](docs/setup-api.md).

## Architecture

```text
Agent --HTTPS MCP--> controlplane --official API--> AutoDL
                         |
Human --HTTPS Web--------+--> PostgreSQL
                         |         |
                         |     watchdog
                         |
                         +--outbound HTTPS<--gemcp-node --Docker--> NVIDIA GPU
                         |
                         +--outbound SSH--> cloud instance --host process--> argv (GPU optional)
                         |
                         +--loopback process--> local CPU fixture (development)
```

The Vue frontend is embedded in the Go release binary. Redis, Kubernetes, and a separate frontend runtime are not required.

## Docs

| Topic | Where |
| --- | --- |
| Graph contract | [docs/graph-contract.md](docs/graph-contract.md) |
| MCP tools and scopes | [docs/mcp.md](docs/mcp.md) |
| Owner / Agent handoffs | [guides/owner-mcp.md](guides/owner-mcp.md), [guides/agent-mcp.md](guides/agent-mcp.md) |
| Research workbench | [docs/research-workbench.md](docs/research-workbench.md) |
| Tester checklist | [docs/tester-brief.md](docs/tester-brief.md) |
| Release notes | [docs/releases.md](docs/releases.md) |
| Roadmap | [docs/roadmap.md](docs/roadmap.md) |
| Cloud SSH | [docs/ssh-cloud-nodes.md](docs/ssh-cloud-nodes.md) |
| Self-hosted nodes | [docs/self-hosted-nodes.md](docs/self-hosted-nodes.md) |
| Provider operations | [docs/provider-operations.md](docs/provider-operations.md) |
| Repositories | [docs/repositories.md](docs/repositories.md) |
| Web console | [docs/web-console.md](docs/web-console.md) |
| Execution and shutdown | [docs/execution.md](docs/execution.md) |
| Architecture | [docs/architecture.md](docs/architecture.md) |

Hosted copies of the Owner and Agent guides are also `/docs/owner-mcp.md` and `/docs/agent-mcp.md` on a running control plane. Connect an Agent with the [MCP client guide](docs/mcp.md) after [Agent Token management](docs/agent-tokens.md). Review [diagnostics](docs/diagnostics.md) before a paid smoke test, [finance](docs/finance.md) before adjusting capacity, and [notifications](docs/notifications.md) before arming SMTP.

Phase-zero AutoDL reads:

```bash
./bin/gemcp phase0 read --backend pro
./bin/gemcp phase0 read --backend elastic --region westDC2
./bin/gemcp phase0 read --backend private
```

A live Job probe is separately gated. Read [Phase-zero validation](docs/phase-zero.md) first.

## Security

No real AutoDL, SMTP, Git, Runner, Agent setup, Node setup, or experiment secret belongs in this repository. Provider, SMTP, and Git private credentials are encrypted at rest. Agent and Node setup codes and their long-lived Tokens are stored only as HMAC digests. AutoDL Runner Tokens are Attempt-scoped and never passed to the user command environment. Self-hosted workload containers receive no Gemcp credential.

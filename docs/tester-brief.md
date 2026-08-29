# Tester brief (Jiyao)

Linear [XIN-28](https://linear.app/xinrunli/issue/XIN-28/gemcp-development) asked Jiyao Pu to test Gemcp. This is the checklist. It is not a product spec.

快速开始：测 `main`（当前 `VERSION` 为 `0.20.0`），**不要**用名为 `Jiyao` 的分支。在仓库根目录运行 `./scripts/bootstrap-local.sh`，再 `./scripts/dev-serve.sh`。第二个终端跑 `./scripts/local-http-smoke.sh` 和 `./scripts/local-cpu-loop.sh`（CPU 五条流，不需要 NVIDIA）。不要手抄 `.env.example`（那是生产 Compose 模板），也不要用 Debian/Ubuntu apt 里的 Go 当编译器版本要求。下面英文是完整步骤。

## Which tree

| Use | Do not use |
| --- | --- |
| `main` (current `VERSION` is `0.20.0`) | Branch `Jiyao` (frozen at v0.15.1; no Research / Graph / Lab split) |
| This repo: `git@github.com:XR-Lee/Gemcp.git` (private; you need GitHub access) | The public Ruby gem `baweaver/gemcp` |
| `./scripts/bootstrap-local.sh` then `./scripts/dev-serve.sh` | Copying `.env.example` and running `./bin/gemcp serve` with only `GEMCP_DATABASE_URL` on the command line |

Confirm after checkout:

```bash
git fetch --tags origin
git checkout main
git pull origin main
git rev-parse HEAD
cat VERSION
```

`VERSION` should match the current release file (`0.20.0` on this tree). The running binary reports that version plus the commit you built. `alpha-0.19` is an older published tag, not the local-HTTP path.

## Local start (HTTP)

Prerequisites the bootstrap script will check or install:

- A Go 1.21+ command so the `go.mod` pin (`1.26.6`) can download the official toolchain. Debian 13 / Ubuntu apt Go is enough as a bootstrap compiler; it is not the required compile version.
- Node.js 22+
- PostgreSQL 16+ on `127.0.0.1:5432` (Debian 13 apt 17 and Ubuntu 24.04 apt 16 both work). Production Compose still uses PostgreSQL 18. No GPU and no NVIDIA Container Toolkit.

```bash
./scripts/bootstrap-local.sh
./scripts/dev-serve.sh
```

`bootstrap-local.sh` writes repo-root `.env` from `deploy/env.local.example`, generates `GEMCP_MASTER_KEY` and `GEMCP_BOOTSTRAP_TOKEN`, creates the `gemcp` role/database, and builds `./bin/gemcp`. `gemcp serve` reads that `.env` (process environment still wins). `GEMCP_AUTO_MIGRATE` is true for development so `/api/v1/setup/status` does not 500 while `/healthz` and `/readyz` return 200. The local template now also turns on `GEMCP_SCHEDULER_ENABLED`, `GEMCP_SSH_CLOUD_ENABLED`, and `GEMCP_LOCAL_PROCESS_ENABLED` so the CPU loop can dispatch a host process on `127.0.0.1` without NVIDIA. Development serve defaults those three on when they are omitted from the environment. If you already have a `.env` that copied the production example (`=false`), re-run bootstrap — it rewrites the three flags to true — and restart serve.

Do **not** copy `.env.example` for this path. That file is the production Compose template (`GEMCP_ENV=production`, `GEMCP_SECURE_COOKIES=true`, Compose-only `GEMCP_DATABASE_URL`). The old documented command that only exported `GEMCP_DATABASE_URL` never loaded `.env`, so serve died on the master-key placeholder or came up without migrations.

In a second terminal:

```bash
./scripts/local-http-smoke.sh
./scripts/local-cpu-loop.sh
```

`local-http-smoke.sh` checks `/healthz`, `/readyz`, `/api/v1/version`, `/api/v1/setup/status`, runs first-run setup with `skip_provider` when needed, and `POST /mcp` initialize. `local-cpu-loop.sh` then hits the five no-NVIDIA flows: register compute / environment / dataset (Cloud SSH loopback + `modelnet40-mini`), schedule through Gemcp, record the assumption vs sub-assumption graph, wait until `get_experiment` shows a scraped heartbeat, and pull the result back onto the graph with a `CLI_DECISION`. Open `http://127.0.0.1:8080` and use the Owner email/password from `.env` (`GEMCP_DEV_OWNER_*`).

Compose (`deploy/`) is for a tunneled HTTPS host. Do not expose port 8080 to the public Internet. Do not require a GPU to finish this HTTP + MCP check.

## First-run and console

The first visit is setup (Owner → Compute → Project), not login. `./scripts/local-http-smoke.sh` can do the same `skip_provider` POST if you have not opened the browser yet.

- Paste the bootstrap token from `.env` (`GEMCP_BOOTSTRAP_TOKEN`).
- In Compute, explicitly skip AutoDL. No GPU or Provider token is required for this path. Add AutoDL later in Lab → Provider when it is needed.
- Copy the one-time Agent Token shown at the end. Gemcp will not show it again. The smoke script stores it in `.gemcp-local-agent-token` (gitignored).

After setup, the home view is **Research** (Study, plan, Graph). **Evidence** lists Experiments. **Lab** is Diagnostics, Finance, Project, Agents, Provider, Alerts. **Nodes** appears when Self-hosted or Cloud SSH is enabled.

Language toggle is on the top bar (Chinese / English).

## What to try first (no GPU required)

These should work on a fresh local control plane:

1. Create a Study from Research. Recording a Graph node must **not** start a workload.
2. Open Evidence. Experiments that never entered the Graph are marked orphaned. That badge is intended.
3. Open Lab → Project, Agents, Finance, Alerts, Provider. If AutoDL was skipped, Provider should show that it is not configured; saving a real token later enables the live views.
4. From Agents, download the non-secret Owner / Agent guides. The live copies are also `/docs/owner-mcp.md` and `/docs/agent-mcp.md`.
5. Connect an MCP client to `http://127.0.0.1:8080/mcp` (loopback HTTP is supported; production remains HTTPS). The server currently registers **28** tools. Call `get_usage_guide`, `get_research_workspace`, and `get_next_actions`. For the local CPU path, `./scripts/local-cpu-loop.sh` registers a loopback Cloud SSH node, ensures the `host` Environment, binds catalog `modelnet40-mini`, prepares argv against `examples/local-cpu/train.py`, and has the Owner confirm the digest. Do **not** call `submit_prepared_experiment` yourself unless you have that same Owner-confirmed digest.

Grok: `grok mcp list` and `grok mcp doctor gemcp-project`. If doctor says the folder is untrusted, grant trust with top-level `grok --trust` in this directory, then run doctor again. There is no `grok mcp doctor --trust` flag.

Unit / UI checks without a running server:

```bash
make test
make frontend-test
```

## Flags that turn on scheduling and nodes

All three stay off after upgrade on purpose.

| Flag | Effect | Extra requirement |
| --- | --- | --- |
| `GEMCP_SCHEDULER_ENABLED=true` | FIFO dispatch of new Experiments on enabled backends | `GEMCP_PUBLIC_URL` may be credential-free HTTPS or loopback HTTP; AutoDL Runner callbacks still require HTTPS, and paid AutoDL requires a healthy Watchdog |
| `GEMCP_SELF_HOSTED_ENABLED=true` | Node enrollment, Nodes console, `gemcp-node` | A remote node needs a reachable HTTPS `GEMCP_PUBLIC_URL`; Linux x86_64 NVIDIA host; Node Setup clones the **exact commit**, not `v0.19.0` |
| `GEMCP_SSH_CLOUD_ENABLED=true` | Experimental outbound-SSH host process backend | Loopback HTTP is sufficient because the control plane opens SSH and polls the process; no inbound Runner callback is used |
| `GEMCP_LOCAL_PROCESS_ENABLED=true` | Same Cloud SSH observer, but `127.0.0.1` / `localhost` run as a local `sh -c` process | Requires Cloud SSH + scheduler; dummy password `local-process`; no NVIDIA |

A local loopback HTTP process can enable the scheduler and Cloud SSH. `config.Load` also accepts loopback HTTP with Self-hosted enabled, but a remote `gemcp-node` cannot reach the control plane through that loopback address. AutoDL create remains blocked until `GEMCP_PUBLIC_URL` is HTTPS.

Self-hosted Node Setup: hosted page `/node/setup`. Build `make build-node` on the GPU host from the rendered commit. After claim, the Enrollment row is `claimed` while the Node desired state is `pending_verification`; the Owner approves the claimed Enrollment. Trusted workspace is Owner-only (one host directory). Paid AutoDL diagnostics need an explicit confirmation digest.

## Do not file these as regressions

Known unfinished or out of scope for this test:

- **No tracing / OpenTelemetry.** Not implemented.
- **Node protocol is still `v1`.** There is no protocol v2 in this repo.
- **No public-repository URL onboarding.** Registration is still name + SSH URL + Deploy Key + verify.
- **No `gemcp.yaml` named workloads, dataset snapshots, build sessions, or cross-node asset placement.**
- **Scheduler disabled:** accepted prepared and Advanced submissions stay `queued` and create no compute resource. The current local `.env` template turns the scheduler and Cloud SSH on so `local-cpu-loop.sh` can finish. **Nodes hidden:** expected only while both Self-hosted and Cloud SSH flags are false.
- **Provider / Diagnostics / paid AutoDL / two physical GPUs / Watchdog-while-down / SMTP / `/root/autodl-fs` persistence** still need authorized live resources. Failures there without those resources are not product regressions.

## What to report

A useful report is: tree (`git rev-parse HEAD` + `./bin/gemcp version`), how you started (local HTTP vs Compose), which flags were on, what you clicked or which MCP tool you called, expected vs actual, and a screenshot or response body. File surprises against that tree, not against branch `Jiyao`. Local HTTP footguns from [issue #6](https://github.com/XR-Lee/Gemcp/issues/6) (`.env` not loaded, apt Go/Postgres vs documented versions, MCP HTTPS/26-tools/`--trust`) should be gone if you followed this brief.

# Tester brief (Jiyao)

Linear [XIN-28](https://linear.app/xinrunli/issue/XIN-28/gemcp-development) asked Jiyao Pu to test Gemcp. This is the checklist. It is not a product spec.

快速开始：测 `main` 或 tag `alpha-0.17`（版本字符串 `v0.17.0`），**不要**用名为 `Jiyao` 的分支（停在 v0.15.1，没有研究工作台）。本地 HTTP 必须 `GEMCP_ENV=development` 且 `GEMCP_SECURE_COOKIES=false`，并用 `./bin/gemcp keygen` / `bootstrap-token` 替换 `.env` 占位符。调度和自托管默认关闭。下面英文是完整步骤。

## Which tree

| Use | Do not use |
| --- | --- |
| `main`, or the published tag `alpha-0.17` | Branch `Jiyao` (frozen at v0.15.1; no Research / Graph / Lab split) |
| Version string `0.17.0` / `v0.17.0` | Older tags such as `alpha-0.16` |
| This repo: `git@github.com:XR-Lee/Gemcp.git` (private; you need GitHub access) | The public Ruby gem `baweaver/gemcp` |

Confirm after checkout:

```bash
git fetch --tags origin
git checkout main
git pull origin main
git rev-parse HEAD
# expect the same commit as tag alpha-0.17 unless newer commits landed on main
git rev-parse alpha-0.17
```

`VERSION` should read `0.17.0`. The running binary reports that version plus the commit you built.

## Local start (HTTP)

Prerequisites: Go 1.26.6, Node.js 22+, PostgreSQL 18.

```bash
npm --prefix frontend install
make test
make frontend-test
make build

cp .env.example .env
```

`.env.example` is a **production Compose** template. For `http://127.0.0.1:8080`:

1. Replace `GEMCP_MASTER_KEY` with `./bin/gemcp keygen` (must be a real 32-byte key; the placeholder will not start).
2. Replace `GEMCP_BOOTSTRAP_TOKEN` with `./bin/gemcp bootstrap-token` (at least 32 characters).
3. Set `GEMCP_ENV=development` and `GEMCP_SECURE_COOKIES=false`. Secure cookies on HTTP look like a login/setup failure.
4. Point `GEMCP_DATABASE_URL` at local PostgreSQL, for example `postgres://gemcp:gemcp@127.0.0.1:5432/gemcp?sslmode=disable`.
5. Leave `GEMCP_SCHEDULER_ENABLED=false` and `GEMCP_SELF_HOSTED_ENABLED=false` until you have HTTPS and a real backend.

```bash
GEMCP_DATABASE_URL='postgres://gemcp:gemcp@127.0.0.1:5432/gemcp?sslmode=disable' ./bin/gemcp serve
```

Check `GET /healthz`, `GET /readyz`, `GET /api/v1/version`. Open `http://127.0.0.1:8080`.

Compose (`deploy/`) is for a tunneled HTTPS host. Do not expose port 8080 to the public Internet.

## First-run and console

The first visit is setup (Owner → Provider → Project), not login.

- Paste the bootstrap token from `.env`.
- First-run **does not** live-call AutoDL. A non-empty Provider token and image UUID are enough to create the Owner. Provider live views will fail until a real AutoDL Private Cloud token is saved later (Lab → Provider). That is expected.
- Copy the one-time Agent Token shown at the end. Gemcp will not show it again.

After setup, the home view is **Research** (Study, plan, Graph). **Evidence** lists Experiments. **Lab** is Diagnostics, Finance, Project, Agents, Provider, Alerts. **Nodes** appears only when self-hosted is enabled.

Language toggle is on the top bar (Chinese / English).

## What to try first (no GPU required)

These should work on a fresh local control plane:

1. Create a Study from Research. Recording a Graph node must **not** start a workload.
2. Open Evidence. Experiments that never entered the Graph are marked orphaned. That badge is intended.
3. Open Lab → Project, Agents, Finance, Alerts, Provider. Provider query without a real token should fail cleanly.
4. From Agents, download the non-secret Owner / Agent guides. The live copies are also `/docs/owner-mcp.md` and `/docs/agent-mcp.md`.
5. Connect an MCP client to `http://127.0.0.1:8080/mcp` with the Agent Token only if you are comfortable putting a secret in a local client. Call `get_usage_guide`, `get_research_workspace`, and `get_next_actions`. Do **not** call `submit_prepared_experiment` without an Owner-confirmed digest. `prepare_experiment` needs a registered repository and Project defaults.

Unit / UI checks without a running server:

```bash
make test
make frontend-test
```

## Flags that turn on scheduling and nodes

Both stay off after upgrade on purpose.

| Flag | Effect | Extra requirement |
| --- | --- | --- |
| `GEMCP_SCHEDULER_ENABLED=true` | FIFO dispatch of new Experiments (AutoDL and self-hosted) | `GEMCP_PUBLIC_URL` must be a credential-free **HTTPS** origin; Watchdog should be running |
| `GEMCP_SELF_HOSTED_ENABLED=true` | Node enrollment, Nodes console, `gemcp-node` | Same HTTPS `GEMCP_PUBLIC_URL`; Linux x86_64 NVIDIA host; Node Setup clones the **exact commit**, not `v0.17.0` |

A local HTTP process **cannot** enable these flags. `config.Load` rejects non-HTTPS public URLs when either flag is true.

Self-hosted Node Setup: hosted page `/node/setup`. Build `make build-node` on the GPU host from the rendered commit. Pairing stays `pending_verification` until the Owner approves it. Trusted workspace is Owner-only (one host directory). Paid AutoDL diagnostics need an explicit confirmation digest.

## Do not file these as regressions

Known unfinished or out of scope for this test:

- **No tracing / OpenTelemetry.** Not implemented.
- **Node protocol is still `v1`.** There is no protocol v2 in this repo.
- **No Owner-console prepare / confirm form.** Prepared proposals are the Agent MCP path (`prepare_experiment` → human digest → `submit_prepared_experiment`). Owners do not have the same form yet ([roadmap](roadmap.md)).
- **No public-repository URL onboarding.** Registration is still name + SSH URL + Deploy Key + verify.
- **No `gemcp.yaml` named workloads, dataset snapshots, build sessions, or cross-node asset placement.**
- **Scheduler and Nodes hidden / refusing work** while the two flags stay false. Expected.
- **Provider / Diagnostics / paid AutoDL / two physical GPUs / Watchdog-while-down / SMTP / `/root/autodl-fs` persistence** still need authorized live resources. Failures there without those resources are not product regressions.
- **`docs/web-console.md` is older than the Research / Lab split.** Trust the running console and [research-workbench](research-workbench.md).

## What to report

A useful report is: tree (`git rev-parse HEAD` + `./bin/gemcp version`), how you started (local HTTP vs Compose), which flags were on, what you clicked or which MCP tool you called, expected vs actual, and a screenshot or response body. File surprises against that tree, not against branch `Jiyao`.

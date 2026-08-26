# Tester brief (Jiyao)

Linear [XIN-28](https://linear.app/xinrunli/issue/XIN-28/gemcp-development) asked Jiyao Pu to test Gemcp. This is the checklist. It is not a product spec.

快速开始：测 `main` 或 tag `alpha-0.19`（版本字符串 `v0.19.0`），**不要**用名为 `Jiyao` 的分支（停在 v0.15.1，没有研究工作台）。本地 HTTP 必须 `GEMCP_ENV=development` 且 `GEMCP_SECURE_COOKIES=false`，并用 `./bin/gemcp keygen` / `bootstrap-token` 替换 `.env` 占位符。调度、自托管和 Cloud SSH 默认关闭。下面英文是完整步骤。

## Which tree

| Use | Do not use |
| --- | --- |
| `main`, or the published tag `alpha-0.19` | Branch `Jiyao` (frozen at v0.15.1; no Research / Graph / Lab split) |
| Version string `0.19.0` / `v0.19.0` | Older tags such as `alpha-0.17` |
| This repo: `git@github.com:XR-Lee/Gemcp.git` (private; you need GitHub access) | The public Ruby gem `baweaver/gemcp` |

Confirm after checkout:

```bash
git fetch --tags origin
git checkout main
git pull origin main
git rev-parse HEAD
# expect the same commit as tag alpha-0.19 unless newer commits landed on main
git rev-parse alpha-0.19
```

`VERSION` should read `0.19.0`. The running binary reports that version plus the commit you built.

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
5. Leave `GEMCP_SCHEDULER_ENABLED=false`, `GEMCP_SELF_HOSTED_ENABLED=false`, and `GEMCP_SSH_CLOUD_ENABLED=false` through first startup. Enable only the backend you are ready to test.

```bash
GEMCP_DATABASE_URL='postgres://gemcp:gemcp@127.0.0.1:5432/gemcp?sslmode=disable' ./bin/gemcp serve
```

Check `GET /healthz`, `GET /readyz`, `GET /api/v1/version`. Open `http://127.0.0.1:8080`.

Compose (`deploy/`) is for a tunneled HTTPS host. Do not expose port 8080 to the public Internet.

## First-run and console

The first visit is setup (Owner → Compute → Project), not login.

- Paste the bootstrap token from `.env`.
- In Compute, either configure AutoDL with a Provider token and image UUID or explicitly skip it. Skipping creates the Owner and Project without Provider credentials; add AutoDL later in Lab → Provider when it is needed.
- Copy the one-time Agent Token shown at the end. Gemcp will not show it again.

After setup, the home view is **Research** (Study, plan, Graph). **Evidence** lists Experiments. **Lab** is Diagnostics, Finance, Project, Agents, Provider, Alerts. **Nodes** appears when Self-hosted or Cloud SSH is enabled.

Language toggle is on the top bar (Chinese / English).

## What to try first (no GPU required)

These should work on a fresh local control plane:

1. Create a Study from Research. Recording a Graph node must **not** start a workload.
2. Open Evidence. Experiments that never entered the Graph are marked orphaned. That badge is intended.
3. Open Lab → Project, Agents, Finance, Alerts, Provider. If AutoDL was skipped, Provider should show that it is not configured; saving a real token later enables the live views.
4. From Agents, download the non-secret Owner / Agent guides. The live copies are also `/docs/owner-mcp.md` and `/docs/agent-mcp.md`.
5. Connect an MCP client to `http://127.0.0.1:8080/mcp` with the Agent Token only if you are comfortable putting a secret in a local client. Call `get_usage_guide`, `get_research_workspace`, and `get_next_actions`. `prepare_experiment` needs a registered repository and Project defaults. Review its repository, commit, argv, runtime, resource, checks, expiry, and reservation in Evidence; the Owner can confirm that exact digest and start the Experiment. Do **not** call `submit_prepared_experiment` without the same Owner-confirmed digest.

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

A local loopback HTTP process can enable the scheduler and Cloud SSH. `config.Load` also accepts loopback HTTP with Self-hosted enabled, but a remote `gemcp-node` cannot reach the control plane through that loopback address. AutoDL create remains blocked until `GEMCP_PUBLIC_URL` is HTTPS.

Self-hosted Node Setup: hosted page `/node/setup`. Build `make build-node` on the GPU host from the rendered commit. After claim, the Enrollment row is `claimed` while the Node desired state is `pending_verification`; the Owner approves the claimed Enrollment. Trusted workspace is Owner-only (one host directory). Paid AutoDL diagnostics need an explicit confirmation digest.

## Do not file these as regressions

Known unfinished or out of scope for this test:

- **No tracing / OpenTelemetry.** Not implemented.
- **Node protocol is still `v1`.** There is no protocol v2 in this repo.
- **No public-repository URL onboarding.** Registration is still name + SSH URL + Deploy Key + verify.
- **No `gemcp.yaml` named workloads, dataset snapshots, build sessions, or cross-node asset placement.**
- **Scheduler disabled:** accepted prepared and Advanced submissions stay `queued` and create no compute resource. **Nodes hidden:** expected only while both Self-hosted and Cloud SSH flags are false.
- **Provider / Diagnostics / paid AutoDL / two physical GPUs / Watchdog-while-down / SMTP / `/root/autodl-fs` persistence** still need authorized live resources. Failures there without those resources are not product regressions.

## What to report

A useful report is: tree (`git rev-parse HEAD` + `./bin/gemcp version`), how you started (local HTTP vs Compose), which flags were on, what you clicked or which MCP tool you called, expected vs actual, and a screenshot or response body. File surprises against that tree, not against branch `Jiyao`.

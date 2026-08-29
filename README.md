# Gemcp

Gemcp is a private research workbench. Sub-agents run inside Docker on Owner-approved machines; the main surface shows a Study, iteration plan, and research Graph. Provider credentials, node authorization, budgets, lifecycle enforcement, and audit data stay in a separate Lab layer.

Testers (including Jiyao Pu / Linear XIN-28): start at the [tester brief](docs/tester-brief.md). Use `main`. Do not use the stale `Jiyao` branch. Local HTTP is `./scripts/bootstrap-local.sh` then `./scripts/dev-serve.sh`.

## Current release

`v0.20.0` lets a local Agent finish Public Elastic onboarding through Gemcp. `register_dataset_binding` accepts catalog `scanobjectnn-objbg` and allowlisted HTTPS sources. `prepare_experiment` with `runtime_preset=provision` is a confirmed, Gemcp-owned fetch onto `/root/autodl-fs`; Agents still must not write wget, curl, or conda. `register_environment` registers a Provider-visible AutoDL image. Optional `install_dependencies` runs `python -m pip install --user` from the verified commit. Cloud SSH injects registered `GEMCP_DATASET_*`. `get_project_options.onboarding.public_cloud` lists the next MCP steps. Owner confirmation of the digest is unchanged.

`v0.19.0` adds experimental Cloud SSH as a channel and observer. An Agent with `operate_nodes`, or the Owner as a fallback, registers a Linux host; Gemcp encrypts the SSH password or private key, probes connectivity, pins the host key, and starts the Agent's argv as a host process. Agents still use `prepare_experiment` and `submit_prepared_experiment` and never receive SSH material back. The Owner confirms a digest of host, user, cwd, and argv. There is no Docker install, image lock, or required GPU. Loopback HTTP can start the scheduler; AutoDL dispatch still requires HTTPS. Cloud SSH, Self-hosted, and AutoDL do not fall back to one another.

`v0.18.0` adds AutoDL Public Elastic (`autodl_elastic`) as a production Job backend beside Private Cloud. First-run setup and Provider token rotation bind the Developer Token only to `https://api.autodl.com` or `https://private.autodl.com`. Public Elastic uses regional GPU inventory, wallet visibility, and `container_template.dc_list` plus a CUDA range. It requires an enterprise-verified AutoDL account. Public Pro remains phase-zero read-only and is not a production scheduling fallback.

`v0.17.0` (`alpha-0.17`) makes an imported research repository usable on the Owner home. Import a Study from a registered repo or a GitHub SSH URL. MCP stays directory-scoped; one setup link covers Pi, Codex, OpenCode, Claude Code, and Grok. The Graph is a capped, fullscreen, time-axis canvas: double-click a node for its record, green/red stamps mark success and failure, and the active path ends at the newest linked record. Research opens the latest imported Project and Study.

`v0.16.2` (published git tag `alpha-0.16`; there is no `v0.16.2` tag) turns the research Graph into an MCP execution contract. Agents call `get_next_actions` before spending. `prepare_experiment` binds `from_node_id` into the confirmation digest when a Study exists. `submit_prepared_experiment` writes the run node. `close_run` is the only way to record a result. Experiments that never enter the Graph are marked orphaned on Evidence. The Owner console uses a lineage brand mark and an ink/paper/copper palette. The current Agent and Owner vocabulary is the [Hypothesis–experiment Graph contract](docs/graph-contract.md).

`v0.16.1` replaces the hand-rolled research surface with Vue Flow, Reka UI, VueUse, and Motion. The Graph is now a connected lineage canvas; Study and Project selectors and the create-Study dialog use accessible primitives; live refresh uses VueUse. Recording a Graph node still never starts a workload.

`v0.16.0` splits the Owner console into Research and Lab. Agents maintain Studies, superseded iteration plans, and a typed Graph through `get_research_workspace` and `update_research_workspace`. Recording a Graph node never starts a workload; execution still uses prepared Experiments and isolated Docker sub-agents. Infrastructure pages remain available but no longer occupy the home view.

`v0.15.1` adds an explicit Agent `configure` scope for registering and verifying GitHub SSH repositories in the authenticated Project and declaring dataset paths below an existing Owner-approved trusted workspace root. Dataset declarations cannot authorize a new host root or additional mount. They produce controlled `GEMCP_DATASET_*` variables, enter the immutable Proposal digest, and are checked by `gemcp-node` for existence and symlink containment before launch. Owners can update an existing active Token's scopes without exposing its secret.

`v0.15.0` adds an explicitly Owner-approved trusted workspace mode for permissive Self-hosted development. The Owner selects a Project and Node and enters one host directory; Gemcp derives the GPU, CPU, memory, Environment, and Resource Profile from live inventory. Prepared Experiments may select a public image name, tag, or digest only for this policy. The Node mounts the approved directory read-write at `/gemcp/workspace`, retains the fixed isolation boundary, resolves tags to repository digests before launch, and records successful digests for reuse. Strict digest-pinned runtimes remain the default and unchanged.

`v0.14.2` makes authorized Self-hosted capacity discoverable to Agents immediately after Node registration and Project authorization. `get_project_options` now projects sanitized Node labels, GPU models and memory, agent version, execution modes, heartbeat state, matching-runtime status, and fixed readiness blockers without exposing credentials, machine fingerprints, GPU UUIDs, or storage paths. A missing approved image boundary is reported as `runtime_configuration_required` instead of making the GPU disappear. The Nodes workspace highlights discovered GPUs without a matching runtime and pre-fills all hardware-derived fields while leaving the digest-pinned image for explicit Owner approval.

`v0.14.1` adds release-bound upgrade instructions to every enrolled Self-hosted Node in the Owner console. The generated bilingual handoff pins the control plane's exact tag and full commit, carries no Node credential, blocks on active Assignments, and invokes a new host-side atomic upgrade script. The script preserves enrollment configuration, credential, bbolt state, and storage, refuses to proceed while any managed container remains, and automatically restores the previous binary if the upgraded service does not remain active.

`v0.14.0` adds an Owner operations workspace for live Agent, proposal, and Experiment evidence. Agents report only controlled workflow phases through the bounded `report_agent_activity` MCP tool. AutoDL Runners and Self-hosted Nodes project validated working/output paths, GPU observations, bounded log tails, and metrics onto Attempts and Experiments; Self-hosted cleanup is complete only after a durable post-removal event. The bilingual Experiment detail separates immutable execution requests from observed runtime state, backend lifecycle, Attempts, and timeline evidence. Visible-page polling keeps active views current without overlapping requests. PostgreSQL schema migration is serialized with a session advisory lock so concurrent control-plane startup cannot race Ent migration.

`v0.13.1` accepts the bounded PAX commit header emitted by `git archive` during prepared source inspection and Self-hosted extraction. The accepted global metadata is restricted to a single 40- or 64-character hexadecimal commit comment; all other global tar metadata remains rejected.

`v0.13.0` adds the repository-first prepared Experiment path for MCP Agents. `prepare_experiment` accepts a reviewed argv plus optional repository/ref selectors, resolves a full commit and compatible Project defaults, runs zero-cost source/runtime/backend/budget checks, and returns a short-lived immutable proposal with an exact CNY reservation. After human confirmation, `submit_prepared_experiment` uses the proposal as a server-owned idempotency boundary. Prepared argv executes without `/bin/sh` in the AutoDL Runner and capability-compatible Self-hosted Nodes. The original `submit_experiment` remains the Advanced shell-command compatibility path. The current v0.19 proposal lifetime is two hours.

`v0.12.0` adds an Owner-only Diagnostics workspace for real, bounded AutoDL and Self-hosted backend tests. Fixed GPU-connectivity and PyTorch-CUDA suites run through the normal Experiment and Attempt lifecycle after source, scheduler, budget, image, capacity, callback, and cleanup preflight checks. Paid AutoDL dispatch requires explicit confirmation of a drift-protected immutable proposal; Self-hosted diagnostics remain zero-CNY. Results combine Runner stages, source downloads, backend ownership, metrics, log tails, timeline, cancellation, and cleanup-aware fault guidance.

`v0.11.2` hardens the remaining AutoDL pre-execution path: interrupted source bodies are downloaded again from a clean temporary file, transient `started` callback failures retry within the provisioning deadline, and controlled Bootstrap stages are persisted for `get_experiment` and the Owner console. HTTP errors and redirects remain non-retryable, user workloads still execute at most once, and a reported terminal Bootstrap failure requests immediate Provider cleanup instead of waiting for `provision_timeout`.

`v0.11.1` hardens initial AutoDL Runner startup after an intermittent provisioning timeout: approved runtime prerequisites may initialize for up to five minutes, transient pre-execution Bootstrap downloads are retried without retrying the workload, and credential-free launch-stage markers are written to the durable `gemcp-launch.log` artifact.

`v0.11.0` adds an Owner-only Finance workspace with monthly and Project filters, capacity and charge summaries, daily trends, backend attribution, per-Project analysis, an immutable budget ledger, finance-related audit history, and idempotent internal credit/debit adjustments. These adjustments change Gemcp scheduling capacity only and never transfer AutoDL funds.

`v0.10.5` adds a global Chinese / English console language switch with browser-language defaults, persistent preference, localized operational views and locale-aware dates.

`v0.10.4` adds switchable Chinese and English Node Setup handoffs, language-aware one-time links, and strict `lang` validation through the UI, installer, and node client.

`v0.10.3` turns the hosted Node Setup page into a self-contained coding-Agent handoff rendered with the running control plane's exact release and commit.

`v0.10.2` keeps the documented AutoDL Private Cloud Developer API resource view available when the optional Web-console system-image endpoint requires a browser login session.

`v0.10.1` updates `golang.org/x/text` to `v0.39.0`, resolving reachable vulnerability `GO-2026-5970` reported by the release CI vulnerability gate.

`v0.10.0` adds feature-gated Self-hosted NVIDIA nodes: short-lived pairing, digest-only Node credentials, Project authorization, durable Commands and Events, transactional single-GPU Assignments, zero-CNY runtime profiles, digest-pinned OCI execution, Node-authenticated source transfer, local deadline enforcement, complete node-local logs, bounded result projection, external GPU-use detection, and an Owner Nodes console. Build Sessions, Dataset Snapshots, and cross-node asset placement are not yet exposed.

`v0.9.0` adds one-link Pi enrollment: an Owner sends one short-lived setup link, the Pi Agent installs its own mode-`0600` MCP configuration, discovers and verifies all bounded tools, and only then activates the Owner-selected scopes and lifetime. Setup codes and Agent Tokens remain digest-only in PostgreSQL, provisional credentials are read-only, claim/completion are retry-safe, and paid execution still requires the explicit approval policy exposed by the Agent guide. `v0.8.1` primes authenticated standalone MCP SSE streams so reverse proxies forward them immediately. `v0.8.0` adds production-hosted Owner and Agent MCP guides, an in-console onboarding workflow, a downloadable non-secret Agent handoff, and guide discovery through an MCP Tool, Resource, and Prompt. `v0.7.1` gives every bootstrap and Runner callback a stable `Gemcp-Runner/1` User-Agent so Cloudflare does not reject Python `urllib` with error 1010. `v0.7.0` adds Owner-managed Agent Token issuance, expiry, revocation, one-time MCP JSON export, client-specific integration guidance, and a session-cached Provider view with visible-page background refresh. `v0.6.4` uses a Provider-safe, quote-free Runner launch command that waits for AutoDL's Miniconda interpreter and streams an encoded downloader over standard input. `v0.6.3` wraps the Runner bootstrap in an explicit Provider-side `/bin/sh -lc` command. `v0.6.2` fixes the in-container Runner bootstrap downloader while preserving redirect rejection. `v0.6.1` hardens PostgreSQL 18 bind-root initialization and the documented `v0.5.x` upgrade path. `v0.6.0` adds opt-in production execution for AutoDL Private Cloud: a PostgreSQL-authoritative FIFO scheduler, immutable Attempts, server-side private-source archives, scoped Runner callbacks, managed-resource ownership, local and independent hard deadlines, idempotent cleanup, and an independently deployed Watchdog. Owner operations now include managed deployment stop, confirmed emergency shutdown, service heartbeats, encrypted SMTP settings, and a durable critical-notification outbox.

New dispatch remains disabled after upgrade until `GEMCP_SCHEDULER_ENABLED=true` is explicitly set. Loopback HTTP is enough to start the scheduler; AutoDL create still requires a reachable HTTPS `GEMCP_PUBLIC_URL`. Reconciliation of existing Attempts still runs while dispatch is off; historical queued experiments cannot start automatically. Cloud SSH remains off until `GEMCP_SSH_CLOUD_ENABLED=true`.

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
```

The Vue frontend is embedded in the Go release binary. Redis, Kubernetes, and a separate frontend runtime are not required.

## Local checks

For a first local run, follow the [tester brief](docs/tester-brief.md). The binary reads repo-root `.env` on `serve` / `watchdog`. Development defaults `GEMCP_AUTO_MIGRATE=true`.

Prerequisites for `./scripts/bootstrap-local.sh`: a Go 1.21+ command (the `go.mod` pin is 1.26.6 and will download that toolchain), Node.js 22+, and PostgreSQL 16+ on loopback. Debian/Ubuntu apt Go and PostgreSQL are acceptable bootstrap packages; they are not the production Compose versions. No GPU is required.

```bash
./scripts/bootstrap-local.sh
./scripts/dev-serve.sh
# second terminal:
./scripts/local-http-smoke.sh
./scripts/local-cpu-loop.sh
```

Manual unit checks without serving:

```bash
npm --prefix frontend install
make test
make frontend-test
make build

# Once per browser-test environment:
npx --prefix frontend playwright install chromium
make frontend-e2e
```

`.env.example` remains the production Compose template. Local HTTP uses `deploy/env.local.example` (`GEMCP_ENV=development`, `GEMCP_SECURE_COOKIES=false`, `GEMCP_PUBLIC_URL=http://127.0.0.1:8080`, scheduler + Cloud SSH + `GEMCP_LOCAL_PROCESS_ENABLED` for the CPU loop). Do not start serve with only `GEMCP_DATABASE_URL` on the command line: that ignores `.env`, dies on the master-key placeholder, or returns 200 on `/healthz` while `/api/v1/setup/status` is 500.

The CPU loop seeds `examples/local-cpu/` to `$HOME/gemcp/datasets/modelnet40-mini`, registers compute / environment / dataset through MCP, records a new-assumption vs sub-assumption graph, waits for a scraped heartbeat, and prints `CLI_DECISION=success|failure|new_observation`. No NVIDIA is required.

Health endpoints:

```text
GET /healthz
GET /readyz
GET /api/v1/version
GET /docs/owner-mcp.md
GET /docs/agent-mcp.md
GET /agent/setup
GET /agent/setup/install.mjs
GET /agent/setup/gemcp-tool.mjs
GET /api/v1/projects
GET /api/v1/finance
POST /api/v1/projects/:id/budget-adjustments
GET /api/v1/nodes
POST /api/v1/node-enrollments
POST /api/v1/node-enrollments/:id/approve
DELETE /api/v1/node-enrollments/:id
POST /api/v1/node-enrollments/claim
POST /api/v1/nodes/sync
GET /api/v1/node-assignments/:id/source
GET|POST /api/v1/projects/:id/self-hosted-runtimes
GET|POST /api/v1/projects/:id/agent-tokens
DELETE /api/v1/projects/:id/agent-tokens/:tokenID
POST /api/v1/projects/:id/agent-enrollments
DELETE /api/v1/projects/:id/agent-enrollments/:enrollmentID
POST /api/v1/agent-enrollments/claim
POST /api/v1/agent-enrollments/complete
GET /api/v1/repositories
GET /api/v1/experiments
GET /api/v1/experiments/:id/attempts
GET /api/v1/projects/:id/cost
GET /api/v1/projects/:id/operations
GET|PUT /api/v1/projects/:id/research
GET /api/v1/projects/:id/diagnostics/options
POST /api/v1/projects/:id/diagnostics/preflight
GET|POST /api/v1/projects/:id/diagnostics
GET /api/v1/projects/:id/diagnostics/:runID
POST /api/v1/projects/:id/diagnostics/:runID/cancel
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

A live Job probe is separately gated by a backend-specific JSON specification, a conservative spend cap, and an exact confirmation phrase. Read [Phase-zero validation](docs/phase-zero.md) before enabling either AutoDL execution backend. The sanitized [Private Cloud validation](docs/private-cloud-validation.md) records the earlier Private Cloud observations only; validate Public Elastic against the current account, region, and official API before scheduling.

Generate both required bootstrap credentials on the deployment host:

```bash
./bin/gemcp keygen
./bin/gemcp bootstrap-token
```

Store them only in the protected deployment `.env`. The first-run setup transaction and Session API are documented in [First-run setup](docs/setup-api.md).

After initialization, validate the live credential and inspect resources through [AutoDL Provider operations](docs/provider-operations.md). Register the private repository using the [Owner Web console](docs/web-console.md) or [Private Git repository API](docs/repositories.md), enroll Pi or issue other client credentials through [Agent Token management](docs/agent-tokens.md), then connect an Agent using the [MCP client guide](docs/mcp.md). The embedded [Owner guide](guides/owner-mcp.md) and [Agent handoff](guides/agent-mcp.md) are also served by the production host and exposed from the Agents page. Review [Backend diagnostics](docs/diagnostics.md) before running a paid or Self-hosted smoke test, and [Finance ledger and internal credits](docs/finance.md) before adjusting Project capacity. Before arming execution, follow [Execution and shutdown enforcement](docs/execution.md) and configure [SMTP notifications](docs/notifications.md).

## Deployment

Copy the environment template beside the Compose file and keep it outside Git:

```bash
cp .env.example deploy/.env
cd deploy
docker compose up -d --build
```

Bind the origin to localhost and publish it through the configured Cloudflare Tunnel. Do not expose port 8080 directly to the public Internet. Existing Compose deployments must follow the [PostgreSQL 18 volume upgrade](deploy/README.md#postgresql-18-volume-upgrade) before recreating the database container.

## Security status

No real AutoDL, SMTP, Git, Runner, Agent setup, Node setup, or experiment secret belongs in this repository. Provider, SMTP, and Git private credentials are encrypted at rest. Agent and Node setup codes and their long-lived Tokens are stored only as HMAC digests; setup retry credentials are deterministically derived and never stored recoverably. AutoDL Runner Tokens are Attempt-scoped, stored as HMAC digests plus recoverable ciphertext only until execution finalizes, and never passed to the user command environment. Self-hosted workload containers receive no Gemcp credential. Local phase-zero Token files and reports are Git-ignored and must be mode `0600` inside a mode `0700` directory.

See the [tester brief](docs/tester-brief.md), [Architecture](docs/architecture.md), [Cloud SSH nodes](docs/ssh-cloud-nodes.md), the [Research workbench](docs/research-workbench.md), the [Experiment interface simplification proposal](docs/experiment-interface-simplification.md), and [Roadmap](docs/roadmap.md).

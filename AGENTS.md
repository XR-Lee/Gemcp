# AGENTS.md

This file is the README for **coding agents working in this repository**.

| You are… | Read this instead |
| --- | --- |
| A human Owner or tester | [README.md](README.md) · [README.zh.md](README.zh.md) |
| An MCP Agent connected to a running Gemcp | [guides/agent-mcp.md](guides/agent-mcp.md), or call `get_usage_guide` / read `gemcp://docs/agent-guide` |
| Helping an Owner wire Cursor / Claude / Codex | [guides/owner-mcp.md](guides/owner-mcp.md) |

Do not paste `guides/agent-mcp.md` into the product README. Do not treat this file as the MCP operating contract.

## Repository

Private research control plane: Go service + embedded Vue console + MCP Streamable HTTP. Module `github.com/XR-Lee/Gemcp`. Current version is the `VERSION` file (`0.20.0`). Work on `main`. The frozen `Jiyao` branch is not this product. This is not the public Ruby gem `baweaver/gemcp`.

Shared vocabulary is [docs/graph-contract.md](docs/graph-contract.md). MCP tools, Owner UI, and Agent guides must use those words. Recording a Graph node never starts a workload.

## Everyday commands

```bash
./scripts/bootstrap-local.sh   # .env, Postgres role/db, ./bin/gemcp
./scripts/dev-serve.sh         # http://127.0.0.1:8080
./scripts/local-http-smoke.sh
./scripts/local-cpu-loop.sh    # no NVIDIA

make test
make frontend-test
make build
```

`make frontend-e2e` needs `npx --prefix frontend playwright install chromium` once. Do not copy `.env.example` for local HTTP; that file is the production Compose template. `serve` / `watchdog` read repo-root `.env` (process environment still wins).

## Layout

| Path | What it is |
| --- | --- |
| `cmd/gemcp`, `cmd/gemcp-node` | Binaries |
| `internal/` | Control plane (MCP, research Graph, execution, providers) |
| `frontend/` | Vue Owner console (embedded at release) |
| `guides/*.md` | Hosted Owner / Agent / Node / Pi handoffs (`//go:embed`) |
| `docs/` | Human engineering docs |
| `deploy/` | Production Compose |
| `examples/local-cpu/` | GPU-free fixture used by `local-cpu-loop.sh` |

## Conventions

- Prefer extending an existing MCP tool or Owner view over adding a parallel vocabulary.
- Paid work stays `prepare_experiment` → Owner-confirmed digest → `submit_prepared_experiment` → `close_run`. Do not invent a path that spends from the question node alone.
- The three remote backends (AutoDL, Self-hosted, Cloud SSH) do not fall back to one another.
- Frontend copy is bilingual at the call site: `t('English', '中文')`.
- Changing `guides/agent-mcp.md` or `guides/owner-mcp.md` changes the live `/docs/*.md` routes and `get_usage_guide`. Keep them free of token-shaped examples (`gmc_…`, `Bearer gmc`).
- No real AutoDL, SMTP, Git, Runner, Agent, Node, or experiment secret in the tree. Generated `.env` and `.gemcp-local-agent-token` are gitignored.

## Verification

For a docs-only change, check links and that audience banners still point at the right files. For code, run the `make` targets that cover the package you touched. For UI, exercise the changed console path; Playwright specs live in `frontend/e2e/`.

Before a paid smoke test read [docs/diagnostics.md](docs/diagnostics.md). Testers: [docs/tester-brief.md](docs/tester-brief.md).

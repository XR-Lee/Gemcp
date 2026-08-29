#!/usr/bin/env bash
# Exercise the five local CPU flows: register, schedule, graph, heartbeat, results.
# Requires ./scripts/bootstrap-local.sh, a running ./scripts/dev-serve.sh, and
# ./scripts/local-http-smoke.sh (skip_provider + Agent Token).
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

if [ ! -f "$repo_root/.env" ]; then
  printf '%s\n' "missing .env; run ./scripts/bootstrap-local.sh first" >&2
  exit 1
fi

# shellcheck disable=SC1091
set -a
# shellcheck source=/dev/null
. "$repo_root/.env"
set +a

if [ "${GEMCP_SSH_CLOUD_ENABLED:-}" != "true" ] || [ "${GEMCP_LOCAL_PROCESS_ENABLED:-}" != "true" ] || [ "${GEMCP_SCHEDULER_ENABLED:-}" != "true" ]; then
  printf '%s\n' "local CPU loop requires GEMCP_SSH_CLOUD_ENABLED=true, GEMCP_LOCAL_PROCESS_ENABLED=true, and GEMCP_SCHEDULER_ENABLED=true in .env; restart ./scripts/dev-serve.sh after changing them" >&2
  exit 1
fi

if [ ! -x "$repo_root/bin/gemcp" ]; then
  printf '%s\n' "missing ./bin/gemcp; run ./scripts/bootstrap-local.sh first" >&2
  exit 1
fi

exec python3 "$repo_root/scripts/local-cpu-loop.py" "$repo_root"

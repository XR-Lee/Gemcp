#!/usr/bin/env bash
# Start the local HTTP control plane using repo-root `.env`.
# The binary also reads `.env`; this script sources it so `ps` and child
# tools see the same values.
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

if [ ! -x "$repo_root/bin/gemcp" ]; then
  printf '%s\n' "missing ./bin/gemcp; run ./scripts/bootstrap-local.sh first" >&2
  exit 1
fi

exec "$repo_root/bin/gemcp" serve

#!/usr/bin/env bash
# Prepare a GPU-free local HTTP control plane: toolchain, PostgreSQL, `.env`,
# and `./bin/gemcp`. Testers should run this from a checkout of main.
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

GO_MIN_MAJOR=1
GO_MIN_MINOR=21
NODE_MIN_MAJOR=22
POSTGRES_MIN_MAJOR=16
ENV_TEMPLATE="$repo_root/deploy/env.local.example"

log() { printf '%s\n' "$*"; }
fail() { printf '%s\n' "$*" >&2; exit 1; }

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

version_major() {
  printf '%s' "$1" | sed -n 's/^[^0-9]*\([0-9][0-9]*\).*/\1/p'
}

version_minor() {
  printf '%s' "$1" | sed -n 's/^[^0-9]*[0-9][0-9]*\.\([0-9][0-9]*\).*/\1/p'
}

go_ok() {
  command -v go >/dev/null 2>&1 || return 1
  local raw major minor
  raw=$(go env GOVERSION 2>/dev/null || go version)
  major=$(version_major "$raw")
  minor=$(version_minor "$raw")
  [ -n "$major" ] && [ -n "$minor" ] || return 1
  [ "$major" -gt "$GO_MIN_MAJOR" ] || { [ "$major" -eq "$GO_MIN_MAJOR" ] && [ "$minor" -ge "$GO_MIN_MINOR" ]; }
}

install_go() {
  local version archive prefix
  version=$(tr -d ' \n' <"$repo_root/go.mod" | sed -n 's/.*go\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\).*/\1/p')
  [ -n "$version" ] || version=1.26.6
  prefix="${GEMCP_LOCAL_GO_PREFIX:-$HOME/.local/go-$version}"
  archive="/tmp/go${version}.linux-amd64.tar.gz"
  log "Installing official Go $version into $prefix (Debian/Ubuntu apt Go is too old for go.mod)."
  curl -fsSL "https://go.dev/dl/go${version}.linux-amd64.tar.gz" -o "$archive"
  rm -rf "$prefix"
  mkdir -p "$prefix"
  tar -C "$prefix" --strip-components=1 -xzf "$archive"
  export PATH="$prefix/bin:$PATH"
  go_ok || fail "installed Go is still unusable"
}

ensure_go() {
  if go_ok; then
    log "Go $(go env GOVERSION 2>/dev/null || go version) (go.mod may download the pinned toolchain)."
    return
  fi
  install_go
}

ensure_node() {
  need_cmd node
  need_cmd npm
  local major
  major=$(version_major "$(node --version)")
  [ -n "$major" ] && [ "$major" -ge "$NODE_MIN_MAJOR" ] || fail "Node.js $NODE_MIN_MAJOR+ is required (found $(node --version))"
  log "Node $(node --version)"
}

postgres_major() {
  if command -v psql >/dev/null 2>&1; then
    version_major "$(psql --version)"
    return
  fi
  if command -v postgres >/dev/null 2>&1; then
    version_major "$(postgres --version)"
    return
  fi
  return 1
}

ensure_postgres_packages() {
  if command -v psql >/dev/null 2>&1 && command -v pg_isready >/dev/null 2>&1; then
    return
  fi
  if command -v apt-get >/dev/null 2>&1; then
    log "Installing distro PostgreSQL (16+ is enough; no GPU or Compose NVIDIA runtime)."
    sudo DEBIAN_FRONTEND=noninteractive apt-get update -y
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql postgresql-contrib
    return
  fi
  fail "PostgreSQL client tools are missing and apt-get is unavailable"
}

start_postgres() {
  if pg_isready -q -h 127.0.0.1 -p 5432; then
    return
  fi
  if command -v pg_isready >/dev/null 2>&1 && pg_isready -q; then
    return
  fi
  if command -v systemctl >/dev/null 2>&1; then
    sudo systemctl enable --now postgresql >/dev/null 2>&1 || sudo systemctl start postgresql || true
  fi
  if command -v service >/dev/null 2>&1; then
    sudo service postgresql start || true
  fi
  sleep 1
  pg_isready -q -h 127.0.0.1 -p 5432 || pg_isready -q || fail "PostgreSQL is installed but not accepting connections on 127.0.0.1:5432"
}

psql_as_superuser() {
  if [ "$(id -u)" -eq 0 ]; then
    psql -v ON_ERROR_STOP=1 "$@"
    return
  fi
  if command -v sudo >/dev/null 2>&1 && sudo -n -u postgres psql -c 'SELECT 1' >/dev/null 2>&1; then
    sudo -u postgres psql -v ON_ERROR_STOP=1 "$@"
    return
  fi
  if command -v sudo >/dev/null 2>&1; then
    sudo -u postgres psql -v ON_ERROR_STOP=1 "$@"
    return
  fi
  fail "cannot run psql as the postgres superuser (need sudo -u postgres)"
}

ensure_database() {
  local user password database
  user=${POSTGRES_USER:-gemcp}
  password=${POSTGRES_PASSWORD:-gemcp}
  database=${POSTGRES_DB:-gemcp}
  if PGPASSWORD="$password" psql -h 127.0.0.1 -p 5432 -U "$user" -d "$database" -c 'SELECT 1' >/dev/null 2>&1; then
    log "PostgreSQL database $database is reachable as $user."
    return
  fi
  log "Creating local role $user and database $database."
  psql_as_superuser -c "DO \$\$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '$user') THEN CREATE ROLE $user LOGIN PASSWORD '$password'; END IF; END \$\$;"
  psql_as_superuser -c "ALTER ROLE $user WITH LOGIN PASSWORD '$password';"
  if ! psql_as_superuser -Atc "SELECT 1 FROM pg_database WHERE datname = '$database'" | grep -qx 1; then
    psql_as_superuser -c "CREATE DATABASE $database OWNER $user;"
  fi
  PGPASSWORD="$password" psql -h 127.0.0.1 -p 5432 -U "$user" -d "$database" -c 'SELECT 1' >/dev/null \
    || fail "created PostgreSQL database but TCP login as $user failed (check pg_hba.conf for 127.0.0.1 scram/md5)"
}

replace_env_value() {
  local file=$1 key=$2 value=$3
  python3 - "$file" "$key" "$value" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
key = sys.argv[2]
value = sys.argv[3]
prefix = key + "="
lines = []
found = False
for line in path.read_text().splitlines(True):
    raw = line.lstrip("\ufeff")
    if raw.startswith(prefix):
        lines.append(f"{key}={value}\n")
        found = True
    else:
        lines.append(line)
if not found:
    if lines and not str(lines[-1]).endswith("\n"):
        lines.append("\n")
    lines.append(f"{key}={value}\n")
path.write_text("".join(lines))
PY
}

ensure_env() {
  [ -f "$ENV_TEMPLATE" ] || fail "missing $ENV_TEMPLATE"
  if [ ! -f "$repo_root/.env" ]; then
    cp "$ENV_TEMPLATE" "$repo_root/.env"
    chmod 600 "$repo_root/.env"
    log "Wrote $repo_root/.env from deploy/env.local.example."
  else
    chmod 600 "$repo_root/.env" || true
    log "Keeping existing $repo_root/.env."
  fi

  # shellcheck disable=SC1091
  set -a
  # shellcheck source=/dev/null
  . "$repo_root/.env"
  set +a

  need_cmd python3
  local gemcp=$repo_root/bin/gemcp
  if [ ! -x "$gemcp" ]; then
    mkdir -p "$repo_root/bin"
    go build -trimpath -o "$gemcp" ./cmd/gemcp
  fi
  if [ "${GEMCP_MASTER_KEY:-}" = "generate-with-gemcp-keygen" ] || [ -z "${GEMCP_MASTER_KEY:-}" ]; then
    replace_env_value "$repo_root/.env" GEMCP_MASTER_KEY "$("$gemcp" keygen)"
    log "Generated GEMCP_MASTER_KEY."
  fi
  if [ "${GEMCP_BOOTSTRAP_TOKEN:-}" = "generate-with-gemcp-bootstrap-token" ] || [ -z "${GEMCP_BOOTSTRAP_TOKEN:-}" ]; then
    replace_env_value "$repo_root/.env" GEMCP_BOOTSTRAP_TOKEN "$("$gemcp" bootstrap-token)"
    log "Generated GEMCP_BOOTSTRAP_TOKEN."
  fi
  if ! grep -q '^GEMCP_LOCAL_PROCESS_ENABLED=' "$repo_root/.env"; then
    replace_env_value "$repo_root/.env" GEMCP_SSH_CLOUD_ENABLED true
    replace_env_value "$repo_root/.env" GEMCP_LOCAL_PROCESS_ENABLED true
    replace_env_value "$repo_root/.env" GEMCP_SCHEDULER_ENABLED true
    log "Enabled local CPU loop flags in .env (restart serve if it is already running)."
  fi
  # shellcheck disable=SC1091
  set -a
  # shellcheck source=/dev/null
  . "$repo_root/.env"
  set +a
}

build_controlplane() {
  if [ ! -d "$repo_root/frontend/node_modules" ]; then
    npm --prefix frontend install
  fi
  make build
}

main() {
  ensure_go
  ensure_node
  ensure_postgres_packages
  local major
  major=$(postgres_major || true)
  if [ -n "$major" ] && [ "$major" -lt "$POSTGRES_MIN_MAJOR" ]; then
    fail "PostgreSQL $POSTGRES_MIN_MAJOR+ is required for local HTTP (found $major). Production Compose still uses 18."
  fi
  if [ -n "$major" ]; then
    log "PostgreSQL $major (local HTTP accepts $POSTGRES_MIN_MAJOR+; Compose production remains 18)."
  fi
  start_postgres
  ensure_env
  ensure_database
  build_controlplane
  log
  log "Local HTTP is ready. Next:"
  log "  ./scripts/dev-serve.sh"
  log "  # in another terminal:"
  log "  ./scripts/local-http-smoke.sh"
  log "  ./scripts/local-cpu-loop.sh"
  log "Open http://127.0.0.1:8080 and skip AutoDL on the Compute step, or let the smoke script call skip_provider."
}

main "$@"

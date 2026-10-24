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
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1${2:+ ($2)}"
}

# as_root runs a command as root: directly when the script already is root
# (containers often have no sudo), otherwise through sudo.
as_root() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  else
    sudo "$@"
  fi
}

# as_user runs a command as another local account, such as postgres.
as_user() {
  local user=$1
  shift
  if [ "$(id -un)" = "$user" ]; then
    "$@"
  elif [ "$(id -u)" -eq 0 ] && command -v runuser >/dev/null 2>&1; then
    runuser -u "$user" -- "$@"
  else
    command -v sudo >/dev/null 2>&1 || fail "need sudo to run $1 as $user"
    sudo -u "$user" "$@"
  fi
}

# check_tools fails before any slow step when a tool the later steps call
# outright is missing. Go, Node.js, and PostgreSQL are checked or installed
# by their own steps.
check_tools() {
  need_cmd make "Debian/Ubuntu: sudo apt-get install -y make"
  need_cmd python3 "Debian/Ubuntu: sudo apt-get install -y python3"
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
  [ -n "$version" ] || version=1.26.10
  prefix="${GEMCP_LOCAL_GO_PREFIX:-$HOME/.local/go-$version}"
  archive="/tmp/go${version}.linux-amd64.tar.gz"
  need_cmd curl "needed to download Go $version"
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
    as_root env DEBIAN_FRONTEND=noninteractive apt-get update -y
    as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql postgresql-contrib
    return
  fi
  fail "PostgreSQL client tools are missing and apt-get is unavailable"
}

postgres_ready() {
  pg_isready -q -h 127.0.0.1 -p 5432 || pg_isready -q
}

# start_postgres brings up an installed server. systemctl works only when
# systemd is PID 1, and containers often have neither systemd nor an init
# script, so it falls back to the cluster tools.
start_postgres() {
  if postgres_ready; then
    return
  fi
  if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    as_root systemctl enable --now postgresql >/dev/null 2>&1 || as_root systemctl start postgresql || true
  elif command -v service >/dev/null 2>&1; then
    as_root service postgresql start || true
  fi
  if ! postgres_ready && command -v pg_lsclusters >/dev/null 2>&1; then
    # Debian/Ubuntu postgresql-common: start the cluster on port 5432.
    pg_lsclusters -h | while read -r version cluster port status _; do
      if [ "$port" = 5432 ] && [ "$status" = down ]; then
        log "Starting PostgreSQL cluster $version/$cluster with pg_ctlcluster."
        as_root pg_ctlcluster "$version" "$cluster" start || true
      fi
    done || true
  fi
  if ! postgres_ready && [ -d "${PGDATA:-}" ] && command -v pg_ctl >/dev/null 2>&1; then
    log "Starting PostgreSQL in $PGDATA with pg_ctl."
    as_user "$(stat -c %U "$PGDATA")" pg_ctl -D "$PGDATA" -l "$PGDATA/gemcp-bootstrap.log" -w start || true
  fi
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    if postgres_ready; then
      return
    fi
    sleep 1
  done
  fail "PostgreSQL is installed but not accepting connections on 127.0.0.1:5432. Start it (for example \`sudo pg_ctlcluster <version> main start\`, or set PGDATA so this script can run pg_ctl) and re-run."
}

psql_as_superuser() {
  as_user postgres psql -v ON_ERROR_STOP=1 "$@"
}

tcp_login() {
  PGPASSWORD="$2" psql -h 127.0.0.1 -p 5432 -U "$1" -d "$3" -c 'SELECT 1' >/dev/null 2>&1
}

ensure_database() {
  local user password database others
  user=${POSTGRES_USER:-gemcp}
  password=${POSTGRES_PASSWORD:-gemcp}
  database=${POSTGRES_DB:-gemcp}
  if tcp_login "$user" "$password" "$database"; then
    log "PostgreSQL database $database is reachable as $user."
    return
  fi
  if ! psql_as_superuser -Atc "SELECT 1 FROM pg_roles WHERE rolname = '$user'" | grep -qx 1; then
    log "Creating local role $user."
    psql_as_superuser -c "CREATE ROLE $user LOGIN PASSWORD '$password';"
  elif ! tcp_login "$user" "$password" postgres; then
    # Another local instance may log in with this role's current password, so
    # only a role that owns nothing else is reset.
    others=$(psql_as_superuser -Atc "SELECT string_agg(datname, ', ') FROM pg_database WHERE datdba = (SELECT oid FROM pg_roles WHERE rolname = '$user') AND datname <> '$database'")
    if [ -n "$others" ]; then
      fail "role $user already owns $others and its password is not POSTGRES_PASSWORD from .env; another local instance may use it. Set POSTGRES_PASSWORD to that password, or give this instance its own POSTGRES_USER, and match GEMCP_DATABASE_URL."
    fi
    log "Setting the password of local role $user (it owns no other database)."
    psql_as_superuser -c "ALTER ROLE $user WITH LOGIN PASSWORD '$password';"
  fi
  if ! psql_as_superuser -Atc "SELECT 1 FROM pg_database WHERE datname = '$database'" | grep -qx 1; then
    log "Creating local database $database."
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

load_env() {
  # shellcheck disable=SC1091
  set -a
  # shellcheck source=/dev/null
  . "$repo_root/.env"
  set +a
}

# use_local_port points a fresh .env at another port and its own database, so
# a second checkout can run beside the first one on the same machine.
use_local_port() {
  local port=$1 database
  database="gemcp_$port"
  replace_env_value "$repo_root/.env" GEMCP_ADDRESS ":$port"
  replace_env_value "$repo_root/.env" GEMCP_PORT "$port"
  replace_env_value "$repo_root/.env" GEMCP_PUBLIC_URL "http://127.0.0.1:$port"
  replace_env_value "$repo_root/.env" POSTGRES_DB "$database"
  replace_env_value "$repo_root/.env" GEMCP_DATABASE_URL "postgres://${POSTGRES_USER:-gemcp}:${POSTGRES_PASSWORD:-gemcp}@127.0.0.1:5432/$database?sslmode=disable"
  log "Configured this instance for port $port and database $database (GEMCP_LOCAL_PORT)."
}

ensure_env() {
  [ -f "$ENV_TEMPLATE" ] || fail "missing $ENV_TEMPLATE"
  local port=${GEMCP_LOCAL_PORT:-}
  if [ -n "$port" ] && { ! [ "$port" -ge 1 ] 2>/dev/null || [ "$port" -gt 65535 ]; }; then
    fail "GEMCP_LOCAL_PORT must be a TCP port number (got $port)"
  fi
  if [ ! -f "$repo_root/.env" ]; then
    cp "$ENV_TEMPLATE" "$repo_root/.env"
    chmod 600 "$repo_root/.env"
    log "Wrote $repo_root/.env from deploy/env.local.example."
    load_env
    if [ -n "$port" ] && [ "$port" != 8080 ]; then
      use_local_port "$port"
    fi
  else
    chmod 600 "$repo_root/.env" || true
    log "Keeping existing $repo_root/.env."
    if [ -n "$port" ]; then
      log "GEMCP_LOCAL_PORT only applies when .env is first written; edit GEMCP_ADDRESS, GEMCP_PUBLIC_URL, POSTGRES_DB, and GEMCP_DATABASE_URL in .env instead."
    fi
  fi
  load_env

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
  # Existing local .env files copied from the production example leave these
  # false and then register_ssh_cloud_node returns "Cloud SSH execution is disabled".
  if [ "${GEMCP_ENV:-development}" != "production" ]; then
    replace_env_value "$repo_root/.env" GEMCP_SSH_CLOUD_ENABLED true
    replace_env_value "$repo_root/.env" GEMCP_LOCAL_PROCESS_ENABLED true
    replace_env_value "$repo_root/.env" GEMCP_SCHEDULER_ENABLED true
    log "Enabled local CPU loop flags in .env (restart serve if it is already running)."
  fi
  load_env
}

build_controlplane() {
  if [ ! -d "$repo_root/frontend/node_modules" ]; then
    npm --prefix frontend install
  fi
  make build
}

main() {
  check_tools
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
  log "Open ${GEMCP_PUBLIC_URL:-http://127.0.0.1:8080} and skip AutoDL on the Compute step, or let the smoke script call skip_provider."
}

main "$@"

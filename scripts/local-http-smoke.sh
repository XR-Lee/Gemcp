#!/usr/bin/env bash
# Prove local HTTP serve + skip_provider setup + MCP initialize.
# Requires ./scripts/bootstrap-local.sh and a running ./scripts/dev-serve.sh.
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

origin=${GEMCP_PUBLIC_URL:-http://127.0.0.1:8080}
origin=${origin%/}
token_file=${GEMCP_DEV_AGENT_TOKEN_FILE:-$repo_root/.gemcp-local-agent-token}

need() { command -v "$1" >/dev/null 2>&1 || { printf 'missing %s\n' "$1" >&2; exit 1; }; }
need curl
need python3

curl_json() {
  local method=$1 path=$2
  shift 2
  curl -fsS --max-time 20 -X "$method" "$origin$path" "$@"
}

expect_http() {
  local method=$1 path=$2 want=$3
  shift 3
  local got
  got=$(curl -sS --max-time 20 -o /tmp/gemcp-smoke-body -w '%{http_code}' -X "$method" "$origin$path" "$@")
  if [ "$got" != "$want" ]; then
    printf 'FAIL %s %s: HTTP %s (want %s)\n%s\n' "$method" "$path" "$got" "$want" "$(cat /tmp/gemcp-smoke-body)" >&2
    exit 1
  fi
  printf 'OK  %s %s -> %s\n' "$method" "$path" "$got"
}

expect_http GET /healthz 200
expect_http GET /readyz 200
expect_http GET /api/v1/version 200
expect_http GET /api/v1/setup/status 200

initialized=$(python3 - <<'PY'
import json, pathlib
print(json.loads(pathlib.Path("/tmp/gemcp-smoke-body").read_text())["data"]["initialized"])
PY
)

if [ "$initialized" = "False" ] || [ "$initialized" = "false" ]; then
  [ -n "${GEMCP_BOOTSTRAP_TOKEN:-}" ] || { printf 'GEMCP_BOOTSTRAP_TOKEN is empty\n' >&2; exit 1; }
  python3 - "$origin" "$GEMCP_BOOTSTRAP_TOKEN" \
    "${GEMCP_DEV_ORGANIZATION:-Local Lab}" \
    "${GEMCP_DEV_OWNER_EMAIL:-owner@localhost}" \
    "${GEMCP_DEV_OWNER_PASSWORD:-local-dev-owner-password}" \
    "${GEMCP_DEV_PROJECT_NAME:-Local Project}" \
    "${GEMCP_DEV_PROJECT_SLUG:-local-project}" \
    "$token_file" <<'PY'
import json, pathlib, sys, urllib.error, urllib.request
origin, bootstrap, org, email, password, project, slug, token_file = sys.argv[1:]
payload = {
    "organization_name": org,
    "owner": {"email": email, "password": password},
    "skip_provider": True,
    "project": {
        "name": project,
        "slug": slug,
        "monthly_budget_milli": 100000,
        "max_experiment_milli": 20000,
        "max_concurrency": 1,
        "max_runtime_seconds": 3600,
        "timeout_extension_seconds": 3600,
        "termination_grace_seconds": 60,
    },
    "agent_token_label": "local-smoke",
}
req = urllib.request.Request(
    origin + "/api/v1/setup",
    data=json.dumps(payload).encode(),
    headers={
        "Content-Type": "application/json",
        "X-Gemcp-Bootstrap-Token": bootstrap,
    },
    method="POST",
)
try:
    with urllib.request.urlopen(req, timeout=30) as resp:
        body = json.loads(resp.read().decode())
        status = resp.status
except urllib.error.HTTPError as exc:
    sys.stderr.write(f"FAIL POST /api/v1/setup: HTTP {exc.code}\n{exc.read().decode()}\n")
    sys.exit(1)
token = body["data"]["agent_token"]
pathlib.Path(token_file).write_text(token + "\n")
pathlib.Path(token_file).chmod(0o600)
print(f"OK  POST /api/v1/setup -> {status} skip_provider agent token stored")
PY
else
  printf 'OK  setup already initialized; using %s if present\n' "$token_file"
fi

if [ ! -s "$token_file" ]; then
  printf '%s\n' "no Agent Token at $token_file; re-run smoke on a fresh database or paste the one-time token from setup" >&2
  exit 1
fi

agent_token=$(tr -d '\n' <"$token_file")

python3 - "$origin" \
  "${GEMCP_DEV_OWNER_EMAIL:-owner@localhost}" \
  "${GEMCP_DEV_OWNER_PASSWORD:-local-dev-owner-password}" \
  "$agent_token" <<'PY'
import http.cookiejar, json, sys, urllib.error, urllib.request
origin, email, password, token = sys.argv[1:]
cookies = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookies))
csrf = ""

def call(method, path, payload=None):
    global csrf
    headers = {"Accept": "application/json"}
    data = None
    if payload is not None:
        headers["Content-Type"] = "application/json"
        data = json.dumps(payload).encode()
    if method not in ("GET", "HEAD") and csrf:
        headers["X-CSRF-Token"] = csrf
    req = urllib.request.Request(origin + path, data=data, headers=headers, method=method)
    try:
        with opener.open(req, timeout=20) as resp:
            raw = resp.read().decode()
            return json.loads(raw) if raw.strip() else {}
    except urllib.error.HTTPError as exc:
        sys.stderr.write(f"WARN {method} {path}: HTTP {exc.code} {exc.read().decode()}\n")
        return {}

login = call("POST", "/api/v1/auth/login", {"email": email, "password": password})
csrf = ((login.get("data") or {}).get("csrf_token") or "")
if not csrf:
    print("WARN Owner login skipped; smoke token scopes were not upgraded", flush=True)
    raise SystemExit(0)
listed_projects = call("GET", "/api/v1/projects").get("data")
if isinstance(listed_projects, list):
    projects = listed_projects
else:
    projects = (listed_projects or {}).get("projects") or []
if not projects:
    print("WARN no Owner projects; smoke token scopes were not upgraded", flush=True)
    raise SystemExit(0)
project_id = projects[0]["id"]
listed = call("GET", f"/api/v1/projects/{project_id}/agent-tokens")
payload = listed.get("data") or {}
tokens = payload.get("tokens") if isinstance(payload, dict) else payload
if not isinstance(tokens, list):
    tokens = []
match = None
for item in tokens:
    prefix = item.get("prefix") or ""
    if prefix and token.startswith(prefix):
        match = item
        break
if match is None:
    print("WARN could not match smoke token; scopes were not upgraded", flush=True)
    raise SystemExit(0)
needed = ("read", "submit", "cancel", "configure", "operate_nodes")
current = list(match.get("scopes") or [])
if all(scope in current for scope in needed):
    print("OK  smoke Agent Token already has configure and operate_nodes", flush=True)
    raise SystemExit(0)
call("PATCH", f"/api/v1/projects/{project_id}/agent-tokens/{match['id']}", {"scopes": list(needed)})
print("OK  Owner granted smoke Agent Token scopes", list(needed), flush=True)
PY

python3 - "$origin" "$agent_token" <<'PY'
import json, sys, urllib.error, urllib.request
origin, token = sys.argv[1], sys.argv[2]
session_id = ""

def decode_body(raw, content_type):
    if "text/event-stream" in content_type:
        data_lines = []
        for line in raw.splitlines():
            if line.startswith("data:"):
                data_lines.append(line[5:].lstrip())
        raw = "\n".join(data_lines)
    return json.loads(raw) if raw.strip() else {}

def mcp(method, params=None, ident=1, notification=False):
    global session_id
    payload = {"jsonrpc": "2.0", "method": method}
    if not notification:
        payload["id"] = ident
    if params is not None:
        payload["params"] = params
    headers = {
        "Authorization": "Bearer " + token,
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
    }
    if session_id:
        headers["Mcp-Session-Id"] = session_id
    req = urllib.request.Request(
        origin + "/mcp",
        data=json.dumps(payload).encode(),
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            status = resp.status
            raw = resp.read().decode()
            content_type = resp.headers.get("Content-Type", "")
            session_id = resp.headers.get("Mcp-Session-Id", session_id)
    except urllib.error.HTTPError as exc:
        sys.stderr.write(f"FAIL POST /mcp {method}: HTTP {exc.code}\n{exc.read().decode()}\n")
        sys.exit(1)
    allowed = (202, 204) if notification else (200,)
    if status not in allowed:
        sys.stderr.write(f"FAIL POST /mcp {method}: HTTP {status}\n{raw}\n")
        sys.exit(1)
    if notification:
        return {}
    body = decode_body(raw, content_type)
    if body.get("error"):
        sys.stderr.write(f"FAIL POST /mcp {method}: {body['error']}\n")
        sys.exit(1)
    return body

init = mcp("initialize", {
    "protocolVersion": "2024-11-05",
    "capabilities": {},
    "clientInfo": {"name": "gemcp-local-smoke", "version": "0"},
})
print("OK  POST /mcp initialize -> 200", init.get("result", {}).get("serverInfo", {}), flush=True)
mcp("notifications/initialized", {}, notification=True)
tools = mcp("tools/list", {}, ident=2)
names = [item["name"] for item in tools.get("result", {}).get("tools", [])]
if len(names) != 28:
    sys.stderr.write(f"FAIL tools/list count={len(names)} want 28: {names}\n")
    sys.exit(1)
print(f"OK  POST /mcp tools/list -> 200 count={len(names)}", flush=True)
PY

printf '\nLocal HTTP + skip_provider + MCP initialize succeeded against %s\n' "$origin"

#!/bin/sh
set -eu

fail() {
  printf 'gemcp-node upgrade failed: %s\n' "$1" >&2
  exit 1
}

if [ "$(id -u)" -ne 0 ]; then
  fail 'upgrade-gemcp-node.sh must run as root'
fi

binary=${GEMCP_NODE_BINARY:-}
expected_version=${GEMCP_NODE_EXPECTED_VERSION:-}
expected_commit=${GEMCP_NODE_EXPECTED_COMMIT:-}
storage_root=${GEMCP_NODE_STORAGE_ROOT:-/var/lib/gemcp-node/storage}
installed_binary=/usr/local/bin/gemcp-node
previous_binary=/usr/local/bin/gemcp-node.previous
next_binary=/usr/local/bin/.gemcp-node.next.$$
service=gemcp-node.service

[ -n "$binary" ] || fail 'GEMCP_NODE_BINARY is required'
[ -x "$binary" ] || fail 'the candidate gemcp-node binary is not executable'
[ -n "$expected_version" ] || fail 'GEMCP_NODE_EXPECTED_VERSION is required'
case "$expected_version" in
  *[!0-9A-Za-z.+-]*) fail 'GEMCP_NODE_EXPECTED_VERSION contains unsupported characters' ;;
esac
case "$expected_commit" in
  ''|*[!0-9a-fA-F]*) fail 'GEMCP_NODE_EXPECTED_COMMIT must be hexadecimal' ;;
esac
case "${#expected_commit}" in
  40|64) ;;
  *) fail 'GEMCP_NODE_EXPECTED_COMMIT must contain 40 or 64 hexadecimal characters' ;;
esac
case "$storage_root" in
  /*) ;;
  *) fail 'GEMCP_NODE_STORAGE_ROOT must be an absolute path' ;;
esac
case "$storage_root" in
  *[[:space:],]*) fail 'GEMCP_NODE_STORAGE_ROOT must not contain whitespace or commas' ;;
esac

candidate_version=$("$binary" version) || fail 'the candidate binary did not report its version'
expected_prefix="gemcp-node $expected_version ($expected_commit, "
case "$candidate_version" in
  "$expected_prefix"*) ;;
  *) fail 'the candidate binary does not match the approved release and commit' ;;
esac

[ -x "$installed_binary" ] || fail 'the installed gemcp-node binary was not found'
[ -f /etc/gemcp-node/config.json ] || fail 'the existing Node config was not found'
[ -f /etc/gemcp-node/credential ] || fail 'the existing Node credential was not found'
[ -f /var/lib/gemcp-node/state.db ] || fail 'the existing Node state database was not found'
systemctl cat "$service" >/dev/null 2>&1 || fail 'gemcp-node.service is not installed'

if ! managed_containers=$(docker ps -a --filter label=io.gemcp.managed=true --format '{{.ID}}'); then
  fail 'Docker is unavailable'
fi
[ -z "$managed_containers" ] || fail 'a managed workload container still exists; wait for cleanup before upgrading'

runuser -u gemcp-node -- "$binary" doctor --storage-root "$storage_root"

cleanup() {
  rm -f "$next_binary"
}
trap cleanup EXIT
trap 'cleanup; exit 1' HUP INT TERM

install -m 0755 "$installed_binary" "$previous_binary"
install -m 0755 "$binary" "$next_binary"
mv -f "$next_binary" "$installed_binary"

upgrade_ok=false
if systemctl restart "$service"; then
  sleep 5
  if systemctl is-active --quiet "$service"; then
    upgrade_ok=true
  fi
fi

if [ "$upgrade_ok" != true ]; then
  printf '%s\n' 'new gemcp-node did not stay active; restoring the previous binary' >&2
  systemctl stop "$service" >/dev/null 2>&1 || true
  install -m 0755 "$previous_binary" "$next_binary"
  mv -f "$next_binary" "$installed_binary"
  systemctl restart "$service" || fail 'automatic rollback could not restart gemcp-node.service'
  sleep 5
  systemctl is-active --quiet "$service" || fail 'automatic rollback did not leave gemcp-node.service active'
  fail 'the new binary was rolled back'
fi

installed_version=$("$installed_binary" version) || fail 'the installed binary did not report its version'
[ "$installed_version" = "$candidate_version" ] || fail 'the installed binary differs from the verified candidate'

printf 'GEMCP_NODE_UPGRADE_OK version=%s commit=%s rollback=%s\n' "$expected_version" "$expected_commit" "$previous_binary"

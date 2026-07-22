#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  printf '%s\n' 'install-gemcp-node.sh must run as root' >&2
  exit 1
fi

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
binary=${GEMCP_NODE_BINARY:-"$script_dir/../bin/gemcp-node"}
storage_root=${GEMCP_NODE_STORAGE_ROOT:-/var/lib/gemcp-node/storage}

case "$storage_root" in
  /*) ;;
  *)
    printf '%s\n' 'GEMCP_NODE_STORAGE_ROOT must be an absolute path' >&2
    exit 1
    ;;
esac
case "$storage_root" in
  *[[:space:]]*)
    printf '%s\n' 'GEMCP_NODE_STORAGE_ROOT must not contain whitespace' >&2
    exit 1
    ;;
  *,*)
    printf '%s\n' 'GEMCP_NODE_STORAGE_ROOT must not contain commas' >&2
    exit 1
    ;;
esac

if [ ! -x "$binary" ]; then
  printf 'gemcp-node binary is not executable: %s\n' "$binary" >&2
  exit 1
fi
if ! getent group docker >/dev/null 2>&1; then
  printf '%s\n' 'Docker group is missing; install Docker Engine before gemcp-node' >&2
  exit 1
fi
if ! id gemcp-node >/dev/null 2>&1; then
  useradd --system --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin gemcp-node
fi
usermod -a -G docker gemcp-node

install -m 0755 "$binary" /usr/local/bin/gemcp-node
install -d -m 0700 -o gemcp-node -g gemcp-node /etc/gemcp-node /var/lib/gemcp-node
install -d -m 0700 -o gemcp-node -g gemcp-node "$storage_root"
install -m 0644 "$script_dir/gemcp-node.service" /etc/systemd/system/gemcp-node.service
install -d -m 0755 /etc/systemd/system/gemcp-node.service.d
{
  printf '%s\n' '[Service]'
  printf 'ReadWritePaths=%s\n' "$storage_root"
} > /etc/systemd/system/gemcp-node.service.d/storage.conf
chmod 0644 /etc/systemd/system/gemcp-node.service.d/storage.conf

printf '%s' 'Paste the complete Gemcp node setup link: '
IFS= read -r setup_link
case "$setup_link" in
  https://*/node/setup\#code=gne_* | \
  https://*/node/setup\?lang=zh\#code=gne_* | \
  https://*/node/setup\?lang=en\#code=gne_*) ;;
  *)
    unset setup_link
    printf '%s\n' 'The node setup link is invalid' >&2
    exit 1
    ;;
esac

runuser -u gemcp-node -- /usr/local/bin/gemcp-node doctor --storage-root "$storage_root"
printf '%s\n' "$setup_link" | runuser -u gemcp-node -- /usr/local/bin/gemcp-node enroll --storage-root "$storage_root" -
unset setup_link

systemctl daemon-reload
systemctl enable --now gemcp-node.service
printf '%s\n' 'GEMCP_NODE_INSTALL_OK'

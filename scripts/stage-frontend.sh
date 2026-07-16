#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
rm -rf "$repo_root/internal/web/dist"
mkdir -p "$repo_root/internal/web/dist"
cp -R "$repo_root/frontend/dist/." "$repo_root/internal/web/dist/"

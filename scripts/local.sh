#!/usr/bin/env bash
# Build Strata from this checkout and run it in Docker, set up like a real
# install, to try a branch before merging it. See CONTRIBUTING.md.
set -euo pipefail
cd "$(dirname "$0")/.."

name=${STRATA_LOCAL_NAME:-strata-local}
port=${STRATA_LOCAL_PORT:-8443}
dir=${STRATA_LOCAL_DIR:-$HOME/strata-local}
image=strata:local

up() {
  local version
  version="local-$(git rev-parse --abbrev-ref HEAD)-$(git rev-parse --short HEAD)"
  docker build -t "$image" --build-arg VERSION="$version" .
  mkdir -p "$dir/data" "$dir/config"
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker run -d --name "$name" -p "$port:8080" --user "$(id -u):$(id -g)" \
    --read-only --tmpfs /tmp:size=64m --cap-drop ALL --security-opt no-new-privileges:true \
    -v "$dir/data:/data" -v "$dir/config:/config" "$image" >/dev/null
  for _ in $(seq 30); do
    curl -skf -o /dev/null "https://localhost:$port/" && break
    sleep 1
  done
  echo "Strata $version is running at https://localhost:$port"
  echo "Files: $dir/data"
  docker logs "$name" 2>&1 | grep -o 'SETUP CODE: .*' || echo "Sign in with your passphrase."
}

case "${1:-up}" in
  up) up ;;
  logs) docker logs -f "$name" ;;
  down) docker rm -f "$name" ;;
  reset)
    read -rp "Delete $dir (test files and passphrase)? [y/N] " ok
    [[ $ok == [yY] ]] || exit 1
    docker rm -f "$name" >/dev/null 2>&1 || true
    rm -rf "$dir"
    ;;
  *) echo "usage: $0 [up|logs|down|reset]" >&2; exit 2 ;;
esac

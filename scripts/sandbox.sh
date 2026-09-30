#!/usr/bin/env bash
# A disposable contributor vault/configuration and a binary built from this tree.
set -euo pipefail
repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
root=$(mktemp -d "${TMPDIR:-/tmp}/zn-sandbox.XXXXXX")
server_pid=
cleanup() {
  if [[ -n "$server_pid" ]]; then
    kill -INT "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf -- "$root"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

unset ZENNOTES_VAULT ZENNOTES_SERVER ZENNOTES_REMOTE_TOKEN ZENNOTES_WORKSPACE_SOURCE
export ZENNOTES_CONFIG_DIR="$root/config"
export ZENNOTES_WORKSPACE_SOURCE=terminal
export NO_COLOR=1
cd -- "$repo"
go build -o "$root/zn" ./cmd/zn
"$root/zn" init "$root/vault" --name sandbox --json
"$root/zn" create --title 'Sandbox 日本語' --tag demo --body $'A disposable note.\n\n- [ ] Try the task list\n- [ ] Open the command palette with Ctrl+G\n'

if [[ "${1:-}" == "--server" ]]; then
  shift
  bind=${ZN_SANDBOX_BIND:-127.0.0.1:17878}
  "$root/zn" server install sandbox --vault "$root/vault" --bind "$bind" --no-input
  "$root/zn" server run sandbox > "$root/server.log" 2>&1 &
  server_pid=$!
  IFS= read -r ZENNOTES_REMOTE_TOKEN < "$root/config/servers/sandbox/token"
  export ZENNOTES_REMOTE_TOKEN
  export ZENNOTES_SERVER="http://$bind"
  ready=false
  for ((attempt=0; attempt<50; attempt++)); do
    if "$root/zn" list --json > /dev/null 2>&1; then ready=true; break; fi
    if ! kill -0 "$server_pid" 2>/dev/null; then break; fi
    sleep 0.2
  done
  if [[ "$ready" != true ]]; then
    printf 'Sandbox server failed to start on %s. Choose a free ZN_SANDBOX_BIND.\n' "$bind" >&2
    exit 1
  fi
fi

if [[ "${1:-}" == "--smoke" ]]; then
  "$root/zn" status --json
  "$root/zn" doctor --json
  "$root/zn" config set editor.word_wrap false
  "$root/zn" read 'Sandbox 日本語.md' --json
  "$root/zn" task list --json
  printf 'Sandbox smoke checks passed.\n'
elif [[ $# -ne 0 ]]; then
  printf 'Usage: bash scripts/sandbox.sh [--server] [--smoke]\n' >&2
  exit 2
else
  printf 'Disposable workspace: %s\nExit the TUI with :qa. Everything is removed on exit.\n' "$root"
  unset NO_COLOR
  "$root/zn" tui
fi

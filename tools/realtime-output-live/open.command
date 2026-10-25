#!/bin/bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
"$repo_root/tools/realtime-output-live/build.command"
control=${REALTIME_NATIVE_SSH_CONTROL:-$repo_root/.superpowers/ssh/cloud.sock}
port=${OUTPUT_LIVE_LOCAL_PORT:-18032}
remote_port=${OUTPUT_LIVE_REMOTE_PORT:-18032}
for candidate in "$port" "$remote_port"; do
  case "$candidate" in ''|*[!0-9]*) exit 2;; esac
  if ((candidate<1024 || candidate>65535)); then exit 2; fi
done
created=false
cleanup() {
  if $created; then ssh -S "$control" -O cancel -L "127.0.0.1:$port:127.0.0.1:$remote_port" ubuntu@82.156.48.111 >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT INT TERM
ssh -S "$control" -O forward -L "127.0.0.1:$port:127.0.0.1:$remote_port" ubuntu@82.156.48.111
created=true
health=$(curl -q --fail --silent --show-error --max-time 5 "http://127.0.0.1:$port/healthz")
backend=$(printf '%s' "$health" | python3 -c 'import json,sys; h=json.load(sys.stdin); print("go" if h.get("backend")=="go" else "python" if sys.argv[1]=="18032" and h.get("experiment")=="output-asr-live" and not h.get("backend") else "unknown")' "$remote_port")
"$repo_root/.superpowers/realtime-output-live/OutputLive.app/Contents/MacOS/OutputLive" --port "$port" --remote-port "$remote_port" --backend "$backend" "$@"

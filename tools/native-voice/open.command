#!/bin/bash
# DEPRECATED_PRODUCT_PATH (2026-10-25): legacy cascade test entry only.
# Use tools/realtime-output-live/open.command for the default Go Realtime entry.
# Shared tools/native-voice/Sources and OpusBridge files remain active; do not remove.
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
port=${VOICE_NATIVE_LOCAL_PORT:-18020}
case "$port" in ''|*[!0-9]*) echo 'VOICE_NATIVE_LOCAL_PORT 必须是端口号'; exit 1;; esac
if ((port < 1024 || port > 65535)); then echo '端口范围为1024到65535'; exit 1; fi
"$repo_root/tools/native-voice/build.command"
private_dir="$repo_root/.superpowers/native-voice"
control="$private_dir/tunnel-$$.sock"
shared_control=${VOICE_NATIVE_SSH_CONTROL:-$repo_root/.superpowers/ssh/cloud.sock}
shared=false
forward_created=false
cleanup() {
  if $shared; then
    if $forward_created; then
      ssh -S "$control" -O cancel -L "127.0.0.1:$port:127.0.0.1:18020" ubuntu@82.156.48.111 >/dev/null 2>&1 || true
    fi
  else
    ssh -S "$control" -O exit ubuntu@82.156.48.111 >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM
printf '%s\n' '建立仅本机回环可访问的 SSH 转发；模型凭据保留在云端。'
if [[ -S "$shared_control" ]] && ssh -S "$shared_control" -O check ubuntu@82.156.48.111 >/dev/null 2>&1; then
  shared=true
  control=$shared_control
  ssh -S "$control" -O forward -L "127.0.0.1:$port:127.0.0.1:18020" ubuntu@82.156.48.111
  forward_created=true
else
  ssh -M -S "$control" -o ControlPersist=yes -o ServerAliveInterval=30 -o ExitOnForwardFailure=yes \
    -L "127.0.0.1:$port:127.0.0.1:18020" -fN ubuntu@82.156.48.111
fi
if [[ ${1:-} == --fixtures ]]; then
  ssh -S "$control" -T ubuntu@82.156.48.111 \
    "curl -q --fail --silent --show-error --max-time 5 -H 'Content-Type: application/json' --data '{\"device_id\":\"native-mac\"}' http://127.0.0.1:18020/v1/pair" \
    | "$private_dir/NativeVoice.app/Contents/MacOS/NativeVoice" --url "ws://127.0.0.1:$port/xiaozhi/v1/" "$@"
else
  "$private_dir/NativeVoice.app/Contents/MacOS/NativeVoice" --url "ws://127.0.0.1:$port/xiaozhi/v1/" --pair-on-start --keep-open "$@"
fi

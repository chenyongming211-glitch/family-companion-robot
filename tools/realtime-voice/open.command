#!/bin/bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
port=${REALTIME_NATIVE_LOCAL_PORT:-18030}
case "$port" in ''|*[!0-9]*) echo 'REALTIME_NATIVE_LOCAL_PORT 必须是端口号'; exit 1;; esac
if ((port<1024 || port>65535)); then echo '端口范围为1024到65535'; exit 1; fi
"$repo_root/tools/realtime-voice/build.command"
private_dir="$repo_root/.superpowers/realtime-voice"
control="$private_dir/tunnel-$$.sock"
shared_control=${REALTIME_NATIVE_SSH_CONTROL:-$repo_root/.superpowers/ssh/cloud.sock}
shared=false
forward_created=false
cleanup() {
 if $shared; then
  if $forward_created; then ssh -S "$control" -O cancel -L "127.0.0.1:$port:127.0.0.1:18030" ubuntu@82.156.48.111 >/dev/null 2>&1 || true; fi
 else
  ssh -S "$control" -O exit ubuntu@82.156.48.111 >/dev/null 2>&1 || true
 fi
}
trap cleanup EXIT INT TERM
if [[ -S "$shared_control" ]] && ssh -S "$shared_control" -O check ubuntu@82.156.48.111 >/dev/null 2>&1; then
 shared=true;control=$shared_control
 ssh -S "$control" -O forward -L "127.0.0.1:$port:127.0.0.1:18030" ubuntu@82.156.48.111
 forward_created=true
else
 ssh -M -S "$control" -o ControlPersist=yes -o ServerAliveInterval=30 -o ExitOnForwardFailure=yes \
  -L "127.0.0.1:$port:127.0.0.1:18030" -fN ubuntu@82.156.48.111
fi
"$private_dir/RealtimeVoice.app/Contents/MacOS/RealtimeVoice" --url "ws://127.0.0.1:$port/xiaozhi/v1/" "$@"

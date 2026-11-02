#!/bin/bash
set -euo pipefail
for name in SPEAKER_SSH_TARGET SPEAKER_SSH_KEY; do
  if [[ -z "${!name:-}" ]]; then
    printf '缺少配置：%s\n' "$name" >&2
    exit 1
  fi
done
[[ "$SPEAKER_SSH_TARGET" =~ ^[a-zA-Z0-9_][a-zA-Z0-9_.@:-]*$ ]] || { echo 'SSH目标格式无效' >&2; exit 1; }
[[ "$SPEAKER_SSH_KEY" = /* && -f "$SPEAKER_SSH_KEY" && -r "$SPEAKER_SSH_KEY" ]] || { echo '现有SSH私钥路径无效' >&2; exit 1; }
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
OUTPUT_LIVE_SPEAKER_BUILD=1 "$repo_root/tools/realtime-output-live/build.command"
ulimit -c 0
unset NATIVE_AUDIO_TRACE NATIVE_AUDIO_CORRELATION
ssh_pid=''
app_pid=''
cleanup() {
  if [[ -n "$app_pid" ]]; then kill "$app_pid" 2>/dev/null || true; wait "$app_pid" 2>/dev/null || true; fi
  if [[ -n "$ssh_pid" ]]; then kill "$ssh_pid" 2>/dev/null || true; wait "$ssh_pid" 2>/dev/null || true; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# This process owns exactly these two loopback forwards and no shared SSH master.
ssh -F /dev/null -N -T -i "$SPEAKER_SSH_KEY" \
  -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes \
  -o ExitOnForwardFailure=yes -o ControlMaster=no -o ControlPath=none \
  -o ServerAliveInterval=15 -o ServerAliveCountMax=2 -o ConnectTimeout=10 \
  -L 127.0.0.1:18052:127.0.0.1:18052 \
  -L 127.0.0.1:18053:127.0.0.1:18053 \
  -- "$SPEAKER_SSH_TARGET" >/dev/null 2>&1 &
ssh_pid=$!
healthy() {
  curl -q --fail --silent --max-time 2 --noproxy '*' "http://127.0.0.1:$1/healthz" 2>/dev/null |
    python3 -c 'import json,sys
try:
 h=json.load(sys.stdin)
 expected=sys.argv[1]=="on"
 ok=(h.get("backend")=="go" and h.get("ready") is True and h.get("speaker_test") is True and h.get("speaker_enabled") is expected and h.get("device_memory") is False and h.get("diagnostics_active") is False)
except (ValueError,TypeError,AttributeError):
 ok=False
sys.exit(0 if ok else 1)' "$2"
}
available=false
for attempt in {1..15}; do
  if ! kill -0 "$ssh_pid" 2>/dev/null; then echo '独立SSH转发未建立。' >&2; exit 1; fi
  if healthy 18052 on && healthy 18053 off && kill -0 "$ssh_pid" 2>/dev/null; then available=true; break; fi
  sleep 1
done
if ! $available; then echo '声纹聊天试点的开启/关闭入口未通过核验。' >&2; exit 1; fi
"$repo_root/.superpowers/realtime-speaker-live/OutputLive.app/Contents/MacOS/OutputLive" \
  --speaker-test --port 18052 --speaker-off-port 18053 --remote-port 18052 \
  --backend go --device-profile speaker-observation >/dev/null 2>&1 &
app_pid=$!
wait "$app_pid"
app_pid=''

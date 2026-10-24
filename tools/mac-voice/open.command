#!/bin/bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
port=${VOICE_LOCAL_PORT:-18010}
case "$port" in ''|*[!0-9]*) echo 'VOICE_LOCAL_PORT must be a port number'; exit 1;; esac
if ((port < 1024 || port > 65535)); then echo 'Choose a port between 1024 and 65535'; exit 1; fi
mkdir -p "$repo_root/.superpowers/mac-voice"
chmod 700 "$repo_root/.superpowers/mac-voice"
control="$repo_root/.superpowers/mac-voice/tunnel-$$.sock"
cleanup() { ssh -S "$control" -O exit ubuntu@82.156.48.111 >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM
echo '连接服务器并建立本机回环SSH转发；不会在Mac搭建语音服务。'
ssh -M -S "$control" -o ControlPersist=600 -o ServerAliveInterval=30 -o ExitOnForwardFailure=yes \
  -L "127.0.0.1:$port:127.0.0.1:18010" -fN ubuntu@82.156.48.111
pair_url=$(ssh -S "$control" -T ubuntu@82.156.48.111 \
  "cd /opt/family-robot-livekit-dev/services/livekit-agent && sudo -n /opt/family-robot-livekit-runtime/venv/bin/python -m family_voice.pair --origin http://127.0.0.1:$port")
open -a 'Google Chrome' "$pair_url"
unset pair_url
echo '已打开Chrome。配对链接一次有效；15分钟配对到期后重新运行本入口。'
echo '允许麦克风后先检查电平、试听提示音，再连接和提问。'
read -r -p '请保持此窗口；按回车关闭本次SSH转发。' ignored

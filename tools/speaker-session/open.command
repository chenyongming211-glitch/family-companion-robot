#!/bin/bash
set -euo pipefail
for name in SPEAKER_SSH_TARGET SPEAKER_SSH_KEY SPEAKER_LAB_BINARY SPEAKER_MODEL SPEAKER_MODEL_SHA; do
  if [[ -z "${!name:-}" ]]; then
    printf '缺少配置：%s\n' "$name" >&2
    exit 1
  fi
done
[[ "$SPEAKER_SSH_TARGET" =~ ^[a-zA-Z0-9_][a-zA-Z0-9_.@:-]*$ ]] || { echo 'SSH目标格式无效' >&2; exit 1; }
[[ "$SPEAKER_SSH_KEY" = /* && -f "$SPEAKER_SSH_KEY" ]] || { echo 'SSH私钥路径无效' >&2; exit 1; }
[[ "$SPEAKER_LAB_BINARY" = /* && "$SPEAKER_MODEL" = /* && "$SPEAKER_MODEL_SHA" =~ ^[0-9a-f]{64}$ ]] || { echo '云端实验配置无效' >&2; exit 1; }
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
"$repo_root/tools/speaker-session/build.command"
ulimit -c 0
# Launch directly to preserve only this invocation's explicit configuration.
# Merely opening the window neither connects SSH nor requests microphone access.
exec "$repo_root/.superpowers/speaker-session/SpeakerSession.app/Contents/MacOS/SpeakerSession" >/dev/null 2>&1

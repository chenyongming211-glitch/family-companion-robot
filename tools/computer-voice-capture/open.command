#!/bin/zsh
set -euo pipefail
umask 077
ulimit -c 0
SCRIPT_DIR=${0:A:h}
REPO_DIR=${SCRIPT_DIR:h:h}
for CONFIG_NAME in COMPUTER_CAPTURE_SSH_TARGET COMPUTER_CAPTURE_SSH_KEY COMPUTER_CAPTURE_RECEIVER COMPUTER_CAPTURE_ROOT; do
  CONFIG_VALUE=${(P)CONFIG_NAME:-}
  if [[ -z "$CONFIG_VALUE" || "$CONFIG_VALUE" == *$'\n'* || "$CONFIG_VALUE" == *$'\r'* ]]; then
    print -u2 'Missing or invalid computer capture configuration.'
    exit 2
  fi
done
if [[ ! "$COMPUTER_CAPTURE_SSH_TARGET" =~ '^[A-Za-z0-9_][A-Za-z0-9_.-]*@[A-Za-z0-9][A-Za-z0-9.-]*$' ]]; then
  print -u2 'Invalid SSH target.'; exit 2
fi
for CONFIG_NAME in COMPUTER_CAPTURE_SSH_KEY COMPUTER_CAPTURE_RECEIVER COMPUTER_CAPTURE_ROOT; do
  CONFIG_VALUE=${(P)CONFIG_NAME}
  if [[ "$CONFIG_VALUE" != /* || "/$CONFIG_VALUE/" == */../* ]]; then
    print -u2 'Capture paths must be absolute without parent traversal.'; exit 2
  fi
done
export COMPUTER_CAPTURE_PARTICIPANT=${COMPUTER_CAPTURE_PARTICIPANT:-M01}
if [[ ! "$COMPUTER_CAPTURE_PARTICIPANT" =~ '^M0[1-5]$' ]]; then
  print -u2 'Invalid anonymous participant.'; exit 2
fi
if [[ ! -f "$COMPUTER_CAPTURE_SSH_KEY" || -L "$COMPUTER_CAPTURE_SSH_KEY" ]]; then
  print -u2 'Configured SSH key is unavailable.'; exit 2
fi
APP_DIR="$REPO_DIR/.superpowers/computer-voice-capture/ComputerVoiceCapture.app"
if [[ ! -x "$APP_DIR/Contents/MacOS/ComputerVoiceCapture" ]]; then
  "$SCRIPT_DIR/build.command"
fi
exec "$APP_DIR/Contents/MacOS/ComputerVoiceCapture"

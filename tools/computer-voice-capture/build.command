#!/bin/zsh
set -euo pipefail
umask 077
SCRIPT_DIR=${0:A:h}
REPO_DIR=${SCRIPT_DIR:h:h}
BUILD_DIR="$REPO_DIR/.superpowers/computer-voice-capture"
APP_DIR="$BUILD_DIR/ComputerVoiceCapture.app"
mkdir -p "$APP_DIR/Contents/MacOS"
xcrun swiftc -swift-version 5 -target "$(uname -m)-apple-macosx13.0" \
  "$SCRIPT_DIR/State.swift" "$SCRIPT_DIR/NativeCapture.swift" "$SCRIPT_DIR/Transport.swift" "$SCRIPT_DIR/main.swift" \
  -framework AppKit -framework AVFoundation -o "$APP_DIR/Contents/MacOS/ComputerVoiceCapture"
cp "$SCRIPT_DIR/Info.plist" "$APP_DIR/Contents/Info.plist"
codesign --force --sign - "$APP_DIR"
printf '%s\n' "$APP_DIR"

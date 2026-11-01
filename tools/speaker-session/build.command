#!/bin/bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
source_dir="$repo_root/tools/speaker-session"
out_dir="$repo_root/.superpowers/speaker-session"
mkdir -p "$out_dir/SpeakerSession.app/Contents/MacOS"
chmod 700 "$out_dir"
xcrun swiftc -swift-version 5 -O -framework AppKit -framework AVFoundation \
  "$source_dir/main.swift" "$source_dir/Capture.swift" "$source_dir/Transport.swift" \
  "$repo_root/tools/native-voice/Sources/MicrophonePCM.swift" \
  -o "$out_dir/SpeakerSession.app/Contents/MacOS/SpeakerSession"
cp "$source_dir/Info.plist" "$out_dir/SpeakerSession.app/Contents/Info.plist"
codesign --force --sign - --identifier com.familycompanion.speakersession "$out_dir/SpeakerSession.app" >/dev/null
printf '已构建：%s\n' "$out_dir/SpeakerSession.app"

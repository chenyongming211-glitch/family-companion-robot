#!/bin/bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
src="$repo_root/tools/realtime-output-live"
common="$repo_root/tools/native-voice"
out="$repo_root/.superpowers/realtime-output-live"
opus_root=${NATIVE_OPUS_ROOT:-/usr/local/Cellar/opus/1.6.1}
mkdir -p "$out/OutputLive.app/Contents/MacOS"
chmod 700 "$out"
xcrun clang -O2 -I"$opus_root/include/opus" -c "$common/OpusBridge.c" -o "$out/OpusBridge.o"
xcrun swiftc -swift-version 5 -O -framework AppKit -framework AVFoundation \
  -import-objc-header "$common/OpusBridge.h" \
  "$common/Sources/AudioPipeline.swift" "$common/Sources/MicrophonePCM.swift" \
  "$common/Sources/Opus.swift" "$common/Sources/PlaybackLedger.swift" \
  "$repo_root/tools/realtime-voice/Sources/Fixture.swift" "$src/LiveClient.swift" "$src/main.swift" \
  "$out/OpusBridge.o" -L"$opus_root/lib" -lopus -Xlinker -rpath -Xlinker "$opus_root/lib" \
  -o "$out/OutputLive.app/Contents/MacOS/OutputLive"
cp "$src/Info.plist" "$out/OutputLive.app/Contents/Info.plist"
codesign --force --sign - --identifier com.familycompanion.outputlive "$out/OutputLive.app" >/dev/null
printf '已构建已播位置实验窗口\n'

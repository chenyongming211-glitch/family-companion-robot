#!/bin/bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
source_dir="$repo_root/tools/realtime-voice"
common="$repo_root/tools/native-voice"
out_dir="$repo_root/.superpowers/realtime-voice"
opus_root=${NATIVE_OPUS_ROOT:-/usr/local/Cellar/opus/1.6.1}
if [[ ! -f "$opus_root/include/opus/opus.h" || ! -f "$opus_root/lib/libopus.dylib" ]]; then
  echo '用 NATIVE_OPUS_ROOT 指定已有 libopus 路径；此脚本不安装依赖。' >&2; exit 1
fi
mkdir -p "$out_dir/RealtimeVoice.app/Contents/MacOS"
chmod 700 "$out_dir"
xcrun clang -O2 -I"$opus_root/include/opus" -c "$common/OpusBridge.c" -o "$out_dir/OpusBridge.o"
xcrun swiftc -swift-version 5 "$common/Sources/PlaybackLedger.swift" "$source_dir/Sources/Protocol.swift" "$source_dir/Tests/StateSpec.swift" -o "$out_dir/state-spec"
"$out_dir/state-spec"
xcrun swiftc -swift-version 5 -O -framework AppKit -framework AVFoundation \
  -import-objc-header "$common/OpusBridge.h" \
  "$common/Sources/AudioPipeline.swift" "$common/Sources/AudioDiagnostics.swift" "$common/Sources/MicrophonePCM.swift" \
  "$common/Sources/Opus.swift" "$common/Sources/PlaybackLedger.swift" \
  "$source_dir"/Sources/*.swift "$out_dir/OpusBridge.o" \
  -L"$opus_root/lib" -lopus -Xlinker -rpath -Xlinker "$opus_root/lib" \
  -o "$out_dir/RealtimeVoice.app/Contents/MacOS/RealtimeVoice"
cp "$source_dir/Info.plist" "$out_dir/RealtimeVoice.app/Contents/Info.plist"
codesign --force --sign - --identifier com.familycompanion.realtimevoice "$out_dir/RealtimeVoice.app" >/dev/null
echo "已构建：$out_dir/RealtimeVoice.app"

#!/bin/bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
source_dir="$repo_root/tools/native-voice"
out_dir="$repo_root/.superpowers/native-voice"
opus_root=${NATIVE_OPUS_ROOT:-/usr/local/Cellar/opus/1.6.1}
if [[ ! -f "$opus_root/include/opus/opus.h" || ! -f "$opus_root/lib/libopus.dylib" ]]; then
  echo '找不到已有 libopus。用 NATIVE_OPUS_ROOT 指定已安装路径；本脚本不安装依赖。' >&2; exit 1
fi
mkdir -p "$out_dir/NativeVoice.app/Contents/MacOS"
chmod 700 "$out_dir"
xcrun clang -O2 -I"$opus_root/include/opus" -c "$source_dir/OpusBridge.c" -o "$out_dir/OpusBridge.o"
xcrun swiftc -swift-version 5 -O -framework AppKit -framework AVFoundation \
  -import-objc-header "$source_dir/OpusBridge.h" "$source_dir"/Sources/*.swift "$out_dir/OpusBridge.o" \
  -L"$opus_root/lib" -lopus -Xlinker -rpath -Xlinker "$opus_root/lib" \
  -o "$out_dir/NativeVoice.app/Contents/MacOS/NativeVoice"
cp "$source_dir/Info.plist" "$out_dir/NativeVoice.app/Contents/Info.plist"
codesign --force --sign - --identifier com.familycompanion.nativevoice "$out_dir/NativeVoice.app" >/dev/null
xcrun swiftc "$source_dir/Sources/PlaybackLedger.swift" "$source_dir/Sources/NativeEndpoint.swift" "$source_dir/Tests/LedgerSpec.swift" -o "$out_dir/ledger-spec"
"$out_dir/ledger-spec"
xcrun swiftc -framework AVFoundation "$source_dir/Sources/PlaybackLedger.swift" \
  "$source_dir/Sources/MicrophonePCM.swift" "$source_dir/Tests/MicrophoneSpec.swift" -o "$out_dir/microphone-spec"
"$out_dir/microphone-spec"
xcrun swiftc -swift-version 5 -framework AppKit -framework AVFoundation \
  -import-objc-header "$source_dir/OpusBridge.h" \
  "$source_dir/Sources/AudioPipeline.swift" "$source_dir/Sources/AudioDiagnostics.swift" "$source_dir/Sources/MicrophonePCM.swift" "$source_dir/Sources/DrainPlayer.swift" \
  "$source_dir/Sources/Opus.swift" "$source_dir/Sources/PlaybackLedger.swift" \
  "$source_dir/Sources/NativeEndpoint.swift" "$source_dir/Sources/VoiceClient.swift" "$source_dir/Tests/FixtureSpec.swift" "$out_dir/OpusBridge.o" \
  -L"$opus_root/lib" -lopus -Xlinker -rpath -Xlinker "$opus_root/lib" -o "$out_dir/fixture-spec"
"$out_dir/fixture-spec"
bash "$source_dir/Tests/launcher-spec.sh"
echo "已构建：$out_dir/NativeVoice.app"

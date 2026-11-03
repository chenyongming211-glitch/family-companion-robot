#!/bin/bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
src="$repo_root/tools/realtime-output-live"
common="$repo_root/tools/native-voice"
out="$repo_root/.superpowers/realtime-output-live"
bundle_id=com.familycompanion.outputlive
if [[ ${OUTPUT_LIVE_SPEAKER_BUILD:-0} == 1 ]]; then
  out="$repo_root/.superpowers/realtime-speaker-live"
  bundle_id=com.familycompanion.speakerlive
fi
opus_root=${NATIVE_OPUS_ROOT:-/usr/local/Cellar/opus/1.6.1}
mkdir -p "$out/OutputLive.app/Contents/MacOS"
chmod 700 "$out"
xcrun clang -O2 -I"$opus_root/include/opus" -c "$common/OpusBridge.c" -o "$out/OpusBridge.o"
xcrun clang -O2 -c "$common/EchoCorrelation.c" -o "$out/EchoCorrelation.o"
xcrun swiftc -swift-version 5 -O -framework AppKit -framework AVFoundation \
  -import-objc-header "$common/OpusBridge.h" \
  "$common/Sources/AudioPipeline.swift" "$common/Sources/AudioDiagnostics.swift" "$common/Sources/AudioCorrelationProbe.swift" "$common/Sources/MicrophonePCM.swift" \
  "$common/Sources/Opus.swift" "$common/Sources/PlaybackLedger.swift" \
  "$repo_root/tools/realtime-voice/Sources/Fixture.swift" "$src/DeviceIdentity.swift" "$src/SpeakerObservation.swift" "$src/SpeakerGuide.swift" "$src/LiveClient.swift" "$src/main.swift" \
  "$out/OpusBridge.o" "$out/EchoCorrelation.o" -L"$opus_root/lib" -lopus -Xlinker -rpath -Xlinker "$opus_root/lib" \
  -o "$out/OutputLive.app/Contents/MacOS/OutputLive"
cp "$src/Info.plist" "$out/OutputLive.app/Contents/Info.plist"
if [[ ${OUTPUT_LIVE_SPEAKER_BUILD:-0} == 1 ]]; then
  /usr/libexec/PlistBuddy -c 'Set :CFBundleIdentifier com.familycompanion.speakerlive' "$out/OutputLive.app/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c 'Set :CFBundleName 小满声纹聊天测试' "$out/OutputLive.app/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c 'Set :NSMicrophoneUsageDescription 点击开始后收音，正常聊天并观察本会话临时说话人候选。' "$out/OutputLive.app/Contents/Info.plist"
fi
codesign --force --sign - --identifier "$bundle_id" "$out/OutputLive.app" >/dev/null
if [[ ${OUTPUT_LIVE_SPEAKER_BUILD:-0} == 1 ]]; then
  printf '已构建声纹聊天测试窗口\n'
else
  printf '已构建已播位置实验窗口\n'
fi

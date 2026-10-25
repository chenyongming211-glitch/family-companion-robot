#!/bin/bash
# Mac validation scheduler only. Production audio, Opus, ledger and LiveClient
# sources are compiled unchanged. This does not build or start a local server.
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
common="$repo_root/tools/native-voice"
src="$repo_root/tools/realtime-output-live"
out="$repo_root/.superpowers/realtime-output-soak"
opus_root=${NATIVE_OPUS_ROOT:-/usr/local/Cellar/opus/1.6.1}
mkdir -p "$out/OutputLiveSoak.app/Contents/MacOS"
chmod 700 "$out"
python3 - "$repo_root" "$out" <<'PY'
from pathlib import Path
import sys
root,out=map(Path,sys.argv[1:])
s=(root/'tools/realtime-voice/Sources/Fixture.swift').read_text()
s=s.replace('steps.count<=8','steps.count<=64').replace('.now()+150)', '.now()+1500)')
s=s.replace('func start(){','private var soakStarted=ProcessInfo.processInfo.systemUptime\n func start(){\n  soakStarted=ProcessInfo.processInfo.systemUptime')
s=s.replace('  do{try load()}catch{finish(false)}\n }\n private func finish',
'''  frames=[];offset=0
  queue.asyncAfter(deadline:.now()+25){[weak self] in
   guard let self,self.timer != nil else{return}
   do{try self.load()}catch{self.finish(false)}
  }
 }
 private func finish''')
s=s.replace(' private func finish(_ success:Bool){',''' private func finish(_ success:Bool){
  let remaining=1200-(ProcessInfo.processInfo.systemUptime-soakStarted)
  if success && remaining>0 {
   metadata("realtime.soak_wait",["remaining_s":Int(remaining)])
   queue.asyncAfter(deadline:.now()+remaining){[weak self] in self?.finish(true)}
   return
  }''')
assert 'steps.count<=64' in s and '.now()+1500)' in s and '.now()+25)' in s
(out/'Fixture.swift').write_text(s)
PY
xcrun clang -O2 -I"$opus_root/include/opus" -c "$common/OpusBridge.c" -o "$out/OpusBridge.o"
xcrun swiftc -swift-version 5 -O -framework AppKit -framework AVFoundation \
  -import-objc-header "$common/OpusBridge.h" \
  "$common/Sources/AudioPipeline.swift" "$common/Sources/MicrophonePCM.swift" \
  "$common/Sources/Opus.swift" "$common/Sources/PlaybackLedger.swift" \
  "$out/Fixture.swift" "$src/LiveClient.swift" "$src/main.swift" \
  "$out/OpusBridge.o" -L"$opus_root/lib" -lopus -Xlinker -rpath -Xlinker "$opus_root/lib" \
  -o "$out/OutputLiveSoak.app/Contents/MacOS/OutputLiveSoak"
cp "$src/Info.plist" "$out/OutputLiveSoak.app/Contents/Info.plist"
codesign --force --sign - --identifier com.familycompanion.outputlive.soak "$out/OutputLiveSoak.app" >/dev/null
printf '已构建20分钟合成播放验证客户端（不启用麦克风）\n'

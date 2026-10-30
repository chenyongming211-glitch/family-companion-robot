#!/bin/bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
common="$repo_root/tools/native-voice"
out_dir="$repo_root/.superpowers/realtime-output-probe"
opus_root=${NATIVE_OPUS_ROOT:-/usr/local/Cellar/opus/1.6.1}
control=${REALTIME_NATIVE_SSH_CONTROL:-$repo_root/.superpowers/ssh/cloud.sock}
mkdir -p "$out_dir"
chmod 700 "$out_dir"
xcrun clang -O2 -I"$opus_root/include/opus" -c "$common/OpusBridge.c" -o "$out_dir/OpusBridge.o"
xcrun clang -O2 -c "$common/EchoCorrelation.c" -o "$out_dir/EchoCorrelation.o"
xcrun swiftc -swift-version 5 -O -framework AVFoundation \
  -import-objc-header "$common/OpusBridge.h" \
  "$common/Sources/AudioPipeline.swift" "$common/Sources/AudioDiagnostics.swift" "$common/Sources/AudioCorrelationProbe.swift" "$common/Sources/MicrophonePCM.swift" \
  "$common/Sources/Opus.swift" "$common/Sources/PlaybackLedger.swift" \
  "$repo_root/tools/realtime-output-probe/OutputProbe.swift" "$out_dir/OpusBridge.o" "$out_dir/EchoCorrelation.o" \
  -L"$opus_root/lib" -lopus -Xlinker -rpath -Xlinker "$opus_root/lib" -o "$out_dir/output-probe"
forward_created=false
cleanup() {
  if $forward_created; then ssh -S "$control" -O cancel -L '127.0.0.1:18031:127.0.0.1:18031' ubuntu@82.156.48.111 >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT INT TERM
ssh -S "$control" -O forward -L '127.0.0.1:18031:127.0.0.1:18031' ubuntu@82.156.48.111
forward_created=true
curl -q --fail --silent --show-error --max-time 5 http://127.0.0.1:18031/readyz >/dev/null
"$out_dir/output-probe" ws://127.0.0.1:18031/probe

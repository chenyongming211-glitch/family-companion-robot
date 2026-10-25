#!/bin/bash
# Run on the Mac test terminal only; this never starts a voice service.
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
out="$repo_root/.superpowers/ownership-local-tests"
mkdir -p "$out"
xcrun swiftc -swift-version 5 \
  "$repo_root/tools/native-voice/Sources/PlaybackLedger.swift" \
  "$repo_root/tools/realtime-output-live/DeviceIdentity.swift" \
  "$repo_root/tools/realtime-output-live/tests/DeviceIdentityTests.swift" \
  -o "$out/device-identity-tests"
"$out/device-identity-tests"

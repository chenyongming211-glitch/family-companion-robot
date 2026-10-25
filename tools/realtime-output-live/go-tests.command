#!/bin/bash
# Run on the cloud server only; no credentials or providers required.
set -euo pipefail
cd "$(dirname "$0")/../.."
go_bin=${GO_BIN:-/opt/family-robot/toolchains/go1.27.3/bin/go}
export REALTIME_PYTHON_BASELINE=${REALTIME_PYTHON_BASELINE:-$PWD}
export REALTIME_AUDIO_PY=${REALTIME_AUDIO_PY:-$REALTIME_PYTHON_BASELINE/tools/realtime-output-live/audio.py}
"$go_bin" test -race ./...
"$go_bin" vet ./...
"$go_bin" build ./...

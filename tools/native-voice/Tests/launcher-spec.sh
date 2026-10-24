#!/bin/bash
# Exercise cleanup on a failed shared forward, without opening network/audio.
set -euo pipefail
client_source=$(cd "$(dirname "$0")/.." && pwd)
fixture_root=$(mktemp -d)
trap 'rm -rf "$fixture_root"' EXIT
mkdir -p "$fixture_root/tools/native-voice" "$fixture_root/.superpowers/native-voice" "$fixture_root/mock-bin"
cp "$client_source/open.command" "$fixture_root/tools/native-voice/open.command"
printf '#!/bin/bash\nexit 0\n' > "$fixture_root/tools/native-voice/build.command"
chmod +x "$fixture_root/tools/native-voice/build.command"
export VOICE_NATIVE_SSH_CONTROL="$fixture_root/shared.sock"
python3 - "$VOICE_NATIVE_SSH_CONTROL" <<'PY'
import socket,sys
s=socket.socket(socket.AF_UNIX)
s.bind(sys.argv[1])
s.close()
PY
export NATIVE_LAUNCHER_TEST_LOG="$fixture_root/ssh.log"
cat > "$fixture_root/mock-bin/ssh" <<'SH'
#!/bin/bash
printf '%s\n' "$*" >> "$NATIVE_LAUNCHER_TEST_LOG"
case "$*" in *'-O check'*) exit 0;; *'-O forward'*) exit 1;; *) exit 0;; esac
SH
chmod +x "$fixture_root/mock-bin/ssh"
if PATH="$fixture_root/mock-bin:$PATH" bash "$fixture_root/tools/native-voice/open.command" >/dev/null 2>&1; then
  echo 'FAIL: forwarded despite simulated failure'; exit 1
fi
python3 - "$NATIVE_LAUNCHER_TEST_LOG" <<'PY'
from pathlib import Path
import sys
calls=Path(sys.argv[1]).read_text()
if '-O exit' in calls or '-O cancel' in calls:
    raise SystemExit('FAIL: failed forward modified the shared master or an existing forward')
PY
echo 'PASS launcher: failed shared forwarding preserves existing SSH master'

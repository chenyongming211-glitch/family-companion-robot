"""Characterize current Go resume state through loopback peers; never call a model."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument(
    "--repo-root", type=Path, default=Path(__file__).resolve().parents[2]
)
parser.add_argument("--out", type=Path, required=True)
parser.add_argument("--race", action="store_true")
args = parser.parse_args()
args.out.mkdir(parents=True, exist_ok=False, mode=0o700)
root = args.repo_root.resolve()
template = Path(__file__).with_name("resume_state_test.go.in").resolve()
virtual = root / "internal/gateway/zz_external_resume_audit_test.go"
assert not virtual.exists()
environment = dict(
    os.environ,
    GOPROXY="off",
    GOSUMDB="off",
    GOTOOLCHAIN="local",
    RESUME_AUDIT_CAPTURE=str((args.out / "session-update-capture.json").resolve()),
)
with tempfile.TemporaryDirectory() as temporary:
    overlay = Path(temporary) / "overlay.json"
    overlay.write_text(json.dumps({"Replace": {str(virtual): str(template)}}))
    command = [
        "go",
        "test",
        "-overlay",
        str(overlay),
        "-count=1",
        "-run",
        "^TestResumeAudit",
        "-v",
    ]
    if args.race:
        command.append("-race")
    command.append("./internal/gateway")
    with (args.out / "test.log").open("x") as log:
        result = subprocess.run(
            command,
            cwd=root,
            env=environment,
            stdout=log,
            stderr=subprocess.STDOUT,
            timeout=120,
        )
print(
    json.dumps(
        {
            "returncode": result.returncode,
            "out": str(args.out),
            "characterization_only": True,
            "external_model_called": False,
        }
    )
)
raise SystemExit(result.returncode)

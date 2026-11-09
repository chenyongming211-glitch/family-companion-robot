"""Cloud threaded actual Configure/Worker regression for observed BLE fragmentation."""
from pathlib import Path
import subprocess
import tempfile
import unittest
from test_onboarding_lifecycle import function
class WorkerStartup(unittest.TestCase):
    def test_reserves_internal_stack_before_ble_and_releases_only_after_ready(self):
        root=Path(__file__).resolve().parents[1]
        source=(root/'overlays/onboarding/family_onboarding.cc').read_text()
        host=(root/'tests/onboarding_worker_start_host.cc').read_text()
        host=host.replace('// WORKER',function(source,'void Worker(void*) {')).replace('// CONFIGURE',function(source,'void Configure() {'))
        with tempfile.TemporaryDirectory() as tmp:
            tmp=Path(tmp);(tmp/'test.cc').write_text(host)
            result=subprocess.run(['c++','-std=c++17','-pthread','-Wall','-Wextra','-Werror','-I',str(root/'overlays/onboarding'),str(tmp/'test.cc'),'-o',str(tmp/'test')],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            result=subprocess.run([str(tmp/'test')],capture_output=True,text=True,timeout=10)
            self.assertEqual(result.returncode,0,result.stderr)

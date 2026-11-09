"""Cloud-only M5 preparation and startup guard regression checks."""
import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
ROOT=Path(__file__).resolve().parents[1]
class M5Screen(unittest.TestCase):
    def test_unactivated_m5_loads_bound_screen_blob(self):
        source=(ROOT/'overlays/onboarding/family_onboarding.cc').read_text()
        guard=source[source.index('    bool screen_ready=true;'):source.index('    nvs_close(h); const auto key = Key();')]
        result=subprocess.run(['c++','-E','-P','-x','c++','-DCONFIG_BOARD_TYPE_M5STACK_STOPWATCH=1','-'],input=guard,text=True,capture_output=True)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertIn('LoadBlob(h,"screen_code"',result.stdout)
        self.assertIn('ValidCode(',result.stdout)
        self.assertIn('if (!Activated())',result.stdout)
    @unittest.skipUnless(os.getenv('M5_ORIGINAL_SOURCE'),'requires cloud frozen original candidate')
    def test_preparation_wires_m5_and_changes_only_small_nvs_layout(self):
        spec=importlib.util.spec_from_file_location('prepare',ROOT/'tools/prepare_onboarding.py')
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        self.assertTrue(hasattr(module,'prepare_m5_screen'),'missing M5 screen preparation')
        with tempfile.TemporaryDirectory() as directory:
            import shutil
            tree=Path(directory);original=Path(os.environ['M5_ORIGINAL_SOURCE'])
            for name in ['main/boards/m5stack/stopwatch/m5stack_stopwatch.cc','partitions/v2/16m.csv']:
                target=tree/name;target.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(original/name,target)
            module.prepare_m5_screen(tree)
            board=(tree/'main/boards/m5stack/stopwatch/m5stack_stopwatch.cc').read_text()
            self.assertIn('void SetOnboardingStatus(',board)
            self.assertIn('family_setup_.Initialize(&family_setup_font,width_,height_)',board)
            self.assertIn('SetFaceScreenOn(title==nullptr)',board)
            import csv
            rows={r[0].strip():[v.strip() for v in r] for r in csv.reader((tree/'partitions/v2/16m.csv').read_text().splitlines()) if r and not r[0].startswith('#')}
            self.assertEqual(rows['nvs'][3:5],['0x9000','0x6000'])
            self.assertEqual(rows['otadata'][3:5],['0xf000','0x2000'])
            self.assertEqual(rows['phy_init'][3:5],['0x11000','0x1000'])
            self.assertEqual(rows['ota_0'][3:5],['0x20000','0x3f0000'])
            self.assertEqual(rows['assets'][3:5],['0x800000','8M'])

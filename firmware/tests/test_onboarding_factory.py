"""Cloud integration checks for private NVS candidate generation.
Set ONBOARDING_OFFICIAL and IDF_PATH to the locked source directories to exercise
real official SRP+NVS tooling. No physical identities or flashing.
"""
import json, os, pathlib, stat, subprocess, sys, tempfile, unittest
ROOT=pathlib.Path(__file__).resolve().parents[1]
@unittest.skipUnless(os.getenv('ONBOARDING_OFFICIAL') and os.getenv('IDF_PATH'),'requires cloud official source fixtures')
class FactoryTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.root=pathlib.Path(self.temp.name)
        self.bundle=self.root/'bundle.json';self.out=self.root/'factory.bin'
        self.data={'schema_version':1,'device_id':'dev_'+'a'*32,'mac':'02:00:00:00:00:01','serial':'SYNTHETIC-S3-001','model':'微雪 ESP32-S3','device_key':'a'*43,'claim_code':'0123456789abcdef0123456789abcdef','bootstrap_url':'https://robot.songguoxue.com/api/device/bootstrap','voice_url':'wss://robot.songguoxue.com/xiaozhi/v1/'}
        self.write()
    def write(self):self.bundle.write_text(json.dumps(self.data));self.bundle.chmod(0o600)
    def run_tool(self):return subprocess.run([sys.executable,str(ROOT/'tools/provision_factory_nvs.py'),'--bundle',str(self.bundle),'--output',str(self.out),'--official-srp',os.environ['ONBOARDING_OFFICIAL'],'--idf-source',os.environ['IDF_PATH']],capture_output=True,text=True)
    def test_real_official_nvs_private_no_claim_secret_and_no_overwrite(self):
        result=self.run_tool();self.assertEqual(result.returncode,0,result.stderr);self.assertEqual(self.out.stat().st_size,0x6000);self.assertEqual(stat.S_IMODE(self.out.stat().st_mode),0o600)
        self.assertNotIn(self.data['claim_code'].encode(),self.out.read_bytes()); original=self.out.read_bytes();self.assertNotEqual(self.run_tool().returncode,0);self.assertEqual(self.out.read_bytes(),original)
    def test_waveshare_image_fills_its_nvs_partition(self):
        self.data["model"]="微雪 ESP32-S3";self.write();result=self.run_tool();self.assertEqual(result.returncode,0,result.stderr);self.assertEqual(self.out.stat().st_size,0x6000)
    def test_public_bundle_rejected(self):self.bundle.chmod(0o644);self.assertNotEqual(self.run_tool().returncode,0);self.assertFalse(self.out.exists())
    def test_untrusted_url_rejected(self):self.data['bootstrap_url']='https://example.com/';self.write();self.assertNotEqual(self.run_tool().returncode,0);self.assertFalse(self.out.exists())
    def test_retired_target_rejected(self):self.data['model']='ESP32-C3';self.write();self.assertNotEqual(self.run_tool().returncode,0);self.assertFalse(self.out.exists())
    def test_duplicate_schema_and_no_secret_diagnostics(self):
        self.bundle.write_text(json.dumps(self.data)[:-1]+',"schema_version":1}');r=self.run_tool();self.assertNotEqual(r.returncode,0);self.assertNotIn(self.data['device_key'],r.stderr+r.stdout);self.assertNotIn(self.data['claim_code'],r.stderr+r.stdout)

@unittest.skipUnless(os.getenv('ONBOARDING_OFFICIAL') and os.getenv('IDF_PATH'),'requires cloud official source fixtures')
class ScreenFactoryTests(unittest.TestCase):
    setUp = FactoryTests.setUp
    write = FactoryTests.write
    def run_tool(self):
        if not hasattr(self,'screen'):return FactoryTests.run_tool(self)
        return subprocess.run([sys.executable,str(ROOT/'tools/provision_factory_nvs.py'),'--bundle',str(self.bundle),'--output',str(self.out),'--official-srp',os.environ['ONBOARDING_OFFICIAL'],'--idf-source',os.environ['IDF_PATH'],'--screen-code',str(self.screen)],capture_output=True,text=True)
    def screen_setup(self):
        import struct,zlib
        self.data['model']='微雪 ESP32-S3';self.write();payload=b'\x80'+bytes(9799)
        self.screen=self.root/'screen.frc';self.screen.write_bytes(b'FRC1'+struct.pack('<HH',280,280)+self.data['device_id'].encode()+struct.pack('<I',zlib.crc32(payload))+payload);self.screen.chmod(0o600)
    def test_screen_real_nvs_fits(self):
        self.screen_setup();r=self.run_tool();self.assertEqual(r.returncode,0,r.stderr);raw=self.out.read_bytes();self.assertEqual(len(raw),0x6000);self.assertIn(b'screen_code',raw);self.assertNotIn(self.data['claim_code'].encode(),raw)
    def test_screen_invalid(self):
        self.screen_setup();original=self.screen.read_bytes()
        for offset in (0,4,6,8,44,48):
            bad=bytearray(original);bad[offset]^=1;self.screen.write_bytes(bad)
            self.assertNotEqual(self.run_tool().returncode,0);self.assertFalse(self.out.exists())
        for bad in (original[:-1],original+b'x'):
            self.screen.write_bytes(bad);self.assertNotEqual(self.run_tool().returncode,0);self.assertFalse(self.out.exists())
        self.screen.write_bytes(original);self.screen.chmod(0o644);self.assertNotEqual(self.run_tool().returncode,0)
        self.screen.chmod(0o600);self.data['model']='unsupported';self.write();self.assertNotEqual(self.run_tool().returncode,0)

    def test_retired_m5_factory_issuance_is_rejected(self):
        self.screen_setup();self.data['model']='M5 ESP32-S3';self.write()
        r=self.run_tool();self.assertNotEqual(r.returncode,0);self.assertFalse(self.out.exists())

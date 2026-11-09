"""Gate real effective SDK configuration and real candidate partition capacity."""
import csv, os
from pathlib import Path
import unittest
@unittest.skipUnless(os.getenv('ONBOARDING_CANDIDATE'),'requires a configured cloud S3 candidate')
class EffectiveCandidate(unittest.TestCase):
    def setUp(self):
        self.tree=Path(os.environ['ONBOARDING_CANDIDATE'])
        self.config=dict(line.split('=',1) for line in (self.tree/'sdkconfig').read_text().splitlines() if line.startswith('CONFIG_') and '=' in line)
    def test_own_bootstrap_and_required_security_effective(self):
        self.assertEqual(self.config['CONFIG_OTA_URL'],'"https://robot.songguoxue.com/api/device/bootstrap"')
        for name in ['BT_BLUEDROID_ENABLED','MBEDTLS_HAVE_TIME_DATE','ESP_PROTOCOMM_SUPPORT_SECURITY_VERSION_2','NETWORK_PROV_NETWORK_TYPE_WIFI']:
            self.assertEqual(self.config['CONFIG_'+name],'y')
        for name in ['ESP_PROTOCOMM_SUPPORT_SECURITY_VERSION_0','ESP_PROTOCOMM_SUPPORT_SECURITY_VERSION_1']:
            self.assertNotEqual(self.config.get('CONFIG_'+name),'y')
    def test_real_app_fits_both_ota_slots_without_data_capacity_loss(self):
        path=self.tree/self.config['CONFIG_PARTITION_TABLE_CUSTOM_FILENAME'].strip('"')
        records={}
        end=0
        for row in csv.reader(path.read_text().splitlines()):
            if not row or row[0].lstrip().startswith('#'):continue
            name,kind,sub,offset,size=[v.strip() for v in row[:5]]
            size=int(size[:-1],0)*1024*1024 if size.endswith('M') else int(size,0)
            offset=int(offset,0) if offset else end
            self.assertGreaterEqual(offset,end);end=offset+size;records[name]=(offset,size)
        self.assertLessEqual(end,16*1024*1024)
        actual=(self.tree/'build/xiaozhi.bin').stat().st_size
        for name in ['ota_0','ota_1']:self.assertGreater(records[name][1],actual)
        self.assertLessEqual((self.tree/'build/generated_assets.bin').stat().st_size, records['assets'][1])
        if path.name=='waveshare-xiaoman-16m.csv':
            self.assertEqual(end,16*1024*1024);self.assertEqual(records['otadata'],(0xf000,0x2000));self.assertEqual(records['phy_init'],(0x11000,0x1000));self.assertEqual(records['nvs'],(0x9000,0x6000));self.assertEqual(records['assets'][1],0x600000);self.assertEqual(records['storage'][1],0x300000)

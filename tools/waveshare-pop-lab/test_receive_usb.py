import hashlib
import pathlib
import tempfile
import unittest
from receive_usb import Receiver

class ReceiverTests(unittest.TestCase):
    def make(self):
        temp=tempfile.TemporaryDirectory();self.addCleanup(temp.cleanup)
        return Receiver(pathlib.Path(temp.name)/'new',1)
    def test_exact_private_capture_and_hash(self):
        r=self.make();r.accept({'kind':'capture','phase':0,'samples':2,'rate':24000,'channels':2})
        r.accept({'kind':'data','phase':0,'offset':0,'hex':'01000200'})
        r.accept({'kind':'capture_end','phase':0,'bytes':4})
        r.accept({'kind':'result','line':'WPL_RESULT 0 ok=1 fault=0'})
        r.accept({'kind':'done'})
        s=r.finish();self.assertTrue(s['complete']);self.assertEqual(s['captures']['0']['sha256'],hashlib.sha256(bytes.fromhex('01000200')).hexdigest())
        self.assertEqual((r.root/'phase-0.pcm').stat().st_mode&0o777,0o600)
    def test_gap_rejected(self):
        r=self.make();r.accept({'kind':'capture','phase':0,'samples':2,'rate':24000,'channels':2})
        with self.assertRaises(ValueError):r.accept({'kind':'data','phase':0,'offset':2,'hex':'0100'})
        self.assertFalse(r.finish('expected_gap')['complete'])
    def test_overflow_rejected(self):
        r=self.make();r.accept({'kind':'capture','phase':0,'samples':2,'rate':24000,'channels':2})
        with self.assertRaises(ValueError):r.accept({'kind':'data','phase':0,'offset':0,'hex':'00'*6})
        self.assertFalse(r.finish('expected_overflow')['complete'])
    def test_partial_does_not_claim_complete(self):
        r=self.make();r.accept({'kind':'done'});self.assertFalse(r.finish()['complete'])
    def test_refuses_existing_directory(self):
        r=self.make()
        with self.assertRaises(FileExistsError):Receiver(r.root,1)
        r.finish('expected_existing')
    def test_unknown_message_rejected(self):
        r=self.make()
        with self.assertRaises(ValueError):r.accept({'kind':'private_text','text':'discard'})
        r.finish('expected_unknown')

if __name__=='__main__':unittest.main()

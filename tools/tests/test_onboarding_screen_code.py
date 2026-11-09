"""Cloud-only synthetic screen supply checks."""
import io, struct, sys, unittest, zlib
from pathlib import Path
sys.path.insert(0,str(Path(__file__).parents[1]))
import test_onboarding_mini_code as fixtures
Transport, Response, code = fixtures.Transport, fixtures.Response, fixtures.code
from PIL import Image

def image(size=(280,280), frames=False):
    im=Image.new('RGB',size,'white');im.putpixel((0,0),(0,0,0));im.putpixel((279,279),(0,0,0)) if size==(280,280) else None
    out=io.BytesIO()
    im.save(out,format='PNG',save_all=frames,append_images=[Image.new('RGB',size,'black')] if frames else [])
    return out.getvalue()

class ScreenTests(unittest.TestCase):
    setUp = fixtures.MiniCodeTests.setUp
    write = fixtures.MiniCodeTests.write
    def run_screen(self,data=None):
        t=Transport([Response(),Response(content_type='image/png',data=image() if data is None else data)])
        code.generate(self.bundle,self.config,'trial',self.out,output_format='screen',connection_factory=t)
        return t
    def test_screen_header_pixels_and_request(self):
        t=self.run_screen();raw=self.out.read_bytes()
        self.assertEqual(len(raw),9848);self.assertEqual(raw[:8],b'FRC1'+struct.pack('<HH',280,280));self.assertEqual(raw[8:44],self.bundle_data['device_id'].encode());self.assertEqual(struct.unpack('<I',raw[44:48])[0],zlib.crc32(raw[48:]));self.assertEqual(raw[48],128);self.assertEqual(raw[-1],1);self.assertEqual(sum(raw[48:]),129)
        p=t.requests[1][2];self.assertEqual({k:p[k] for k in ('width','auto_color','line_color','is_hyaline')},{'width':280,'auto_color':False,'line_color':{'r':0,'g':0,'b':0},'is_hyaline':False})
    def test_screen_bad_images(self):
        for data in (image((281,280)),image(frames=True),image()[:-20],b'bad'):
            with self.assertRaises(code.MiniCodeError):self.run_screen(data)
            self.assertFalse(self.out.exists())
    def test_screen_public_parent_and_cancel(self):
        self.root.chmod(0o755)
        with self.assertRaises(code.MiniCodeError):self.run_screen()
        self.root.chmod(0o700)
        from unittest.mock import patch
        with patch.object(code.os,'write',side_effect=KeyboardInterrupt):
            with self.assertRaises(KeyboardInterrupt):self.run_screen()
        self.assertFalse(self.out.exists())

    def test_screen_cli_and_invalid_format(self):
        from unittest.mock import patch
        import contextlib
        t=Transport([Response(),Response(content_type='image/png',data=image())])
        with patch.object(code.http.client,'HTTPSConnection',t), contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(code.main(['--bundle',str(self.bundle),'--wechat-config',str(self.config),'--environment','trial','--output',str(self.out),'--format','screen']),0)
        self.assertEqual(self.out.read_bytes()[:4],b'FRC1');self.out.unlink()
        t=Transport()
        with self.assertRaises(code.MiniCodeError):
            code.generate(self.bundle,self.config,'trial',self.out,output_format='invalid',connection_factory=t)
        self.assertEqual(t.requests,[])

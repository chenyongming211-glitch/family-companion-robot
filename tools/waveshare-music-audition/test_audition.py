import importlib.util
import struct
import json
import shutil
import subprocess
import sys
from types import SimpleNamespace
from unittest.mock import patch
import tempfile
import unittest
import wave
from pathlib import Path

HERE = Path(__file__).resolve().parent

def load(name):
    spec = importlib.util.spec_from_file_location(name, HERE / (name + '.py'))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

class AuditionTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        assert (HERE / 'client.py').is_file(), 'client implementation missing'
        cls.client = load('client')
        cls.prepare = load('prepare_audio')

    def test_wire_crc_and_bounds(self):
        c = self.client
        raw = c.encode_frame(1, 0, 48000, struct.pack('<Q', 1441))
        self.assertEqual(raw.hex(), '464d4131010000000000000080bb00000800000078caf55aa105000000000000')
        self.assertEqual(c.parse_frame(raw), (1, 0, 48000, struct.pack('<Q', 1441)))
        for bad in (raw[:-1], raw + b'x', b'x' + raw[1:], raw[:24] + b'x' * 8):
            with self.assertRaises(ValueError): c.parse_frame(bad)
        for op, arg, data in ((2, 0, b'x'), (2, 0, b''), (2, 0, b'x'*2882),
                              (1, 44100, struct.pack('<Q', 4)), (1, 24000, struct.pack('<Q', 0)),
                              (1, 48000, struct.pack('<Q', 5760001)), (3, 1, b''), (4, 0, b'x')):
            with self.assertRaises(ValueError): c.encode_frame(op, 0, arg, data)

    def transport(self, overrides=None):
        c = self.client
        class Device:
            def __init__(self): self.pending = None; self.frames = []; self.total = 0; self.rate = 24000
            def write(self, raw):
                if self.pending is not None: raise AssertionError('sent without observing prior event')
                op, seq, arg, data = c.parse_frame(raw)
                self.frames.append(op)
                if op == 1: self.rate = arg; kind = 'READY'
                elif op == 2: self.total += len(data)//2; kind = 'ACK'
                elif op == 3: self.rate = 24000; kind = 'DONE'
                else: self.rate = 24000; kind = 'STOPPED'
                self.pending = ((overrides or {}).get(op) or f'FMA1 {seq} {kind} {self.rate} {self.total}\n').encode()
                return len(raw)
            def readline(self):
                result = self.pending or b''; self.pending = None; return result
        return Device()

    def pcm(self, directory, samples=1441):
        path = Path(directory) / 'audio.wav'
        with wave.open(str(path), 'wb') as f:
            f.setnchannels(1); f.setsampwidth(2); f.setframerate(48000); f.writeframes(b'\0\0' * samples)
        return path

    def test_client_exact_counts_and_physical_completion(self):
        d = self.transport(); events = []
        with tempfile.TemporaryDirectory() as directory:
            result = self.client.stream_audio(d, self.pcm(directory), 48000, emit=events.append)
        self.assertEqual(d.frames, [1,2,2,3]); self.assertEqual(result, ('DONE', 24000, 1441))
        self.assertEqual(len(events), 4)

    def test_client_cancellation_at_block_boundary(self):
        d = self.transport()
        with tempfile.TemporaryDirectory() as directory:
            result = self.client.stream_audio(d, self.pcm(directory), 48000,
                                               stop_requested=lambda: len(d.frames) >= 2)
        self.assertEqual(d.frames, [1,2,4]); self.assertEqual(result, ('STOPPED',24000,1440))

    def test_client_physical_button_stop(self):
        d = self.transport({2: 'FMA1 1 STOPPED 24000 0\n'})
        with tempfile.TemporaryDirectory() as directory:
            result = self.client.stream_audio(d, self.pcm(directory), 48000)
        self.assertEqual(result, ('STOPPED',24000,0)); self.assertEqual(d.frames,[1,2])

    def test_client_rejects_mismatch_timeout_and_false_restoration(self):
        for overrides in ({1: 'FMA1 1 READY 48000 0\n'}, {1: 'FMA1 0 READY 24000 0\n'},
                          {2: 'FMA1 1 ACK 48000 1442\n'}, {2: 'FMA1 1 ERROR 48000 0\n'},
                          {3: 'FMA1 3 DONE 48000 1441\n'}, {3: 'FMA1 3 DONE 24000 1440\n'},
                          {1: '\n'}):
            d = self.transport(overrides)
            with tempfile.TemporaryDirectory() as directory:
                with self.assertRaises((ValueError, TimeoutError)):
                    self.client.stream_audio(d, self.pcm(directory), 48000, timeout=0.01)

    def test_client_rejects_truncated_wav(self):
        with tempfile.TemporaryDirectory() as directory:
            p = self.pcm(directory); p.write_bytes(p.read_bytes()[:-2])
            d = self.transport()
            with self.assertRaises(ValueError): self.client.stream_audio(d,p,48000)
            self.assertEqual(d.frames, [])

    def test_response_partial_logs_and_nonmonotonic_count(self):
        c = self.client
        class Partial:
            chunks = [b'boot log\n', b'FMA1 0 RE', b'ADY 48000 0\n']
            def read_until(self, *_args): return self.chunks.pop(0)
        self.assertEqual(c._event(Partial(),1),(0,'READY',48000,0))
        for line in (b'FMA1 0 READY +48000 0\n',b'FMA1 0 READY 48000 -1\n',
                     b'FMA1 0 WHATEVER 48000 0\n',b'FMA1 0 READY 48000 0 trailing\n'):
            with self.assertRaises(ValueError): c.parse_response(line)
        d=self.transport({2:'FMA1 1 ACK 48000 1440\n',3:'FMA1 3 DONE 24000 1441\n'})
        # Second block receives a stale sequence and must never send END.
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError): c.stream_audio(d,self.pcm(directory),48000)
        self.assertEqual(d.frames,[1,2,2])

    def test_client_short_write_and_cancel_restoration(self):
        d=self.transport(); d.write=lambda _raw: 0
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError): self.client.stream_audio(d,self.pcm(directory),48000)
        d=self.transport({4:'FMA1 1 STOPPED 48000 0\n'})
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                self.client.stream_audio(d,self.pcm(directory),48000,stop_requested=lambda:True)

    def test_cli_opens_usb_without_asserting_reset_lines(self):
        class Port:
            dtr=True; rts=True; opened=False
            def open(port):
                self.assertFalse(port.dtr); self.assertFalse(port.rts); port.opened=True
            def __enter__(port): return port
            def __exit__(port,*_args): pass
        port=Port()
        def factory(_url,**kwargs):
            self.assertTrue(kwargs.get('do_not_open'))
            return port
        serial=SimpleNamespace(serial_for_url=factory,SerialException=OSError)
        with patch.dict(sys.modules,{'serial':serial}), patch.object(sys,'argv',
             ['client','--port','/dev/test','--wav','fixture.wav','--rate','24000']), \
             patch.object(self.client.signal,'signal'),patch.object(self.client,'stream_audio'):
            self.client.main()
        self.assertTrue(port.opened)

    def test_cli_rfc_uses_network_deadline(self):
        class Port:
            dtr=True; rts=True; opened=False
            def open(port):
                self.assertFalse(port.dtr); self.assertFalse(port.rts); port.opened=True
            def __enter__(port): return port
            def __exit__(port,*_args): pass
        port=Port()
        url='rfc2217://127.0.0.1:42217?timeout=5'
        def factory(actual_url,**kwargs):
            # pyserial 3.5 RFC2217 rejects a serial write_timeout on open;
            # its URL timeout bounds the underlying network socket instead.
            if kwargs.get('write_timeout') is not None:
                raise NotImplementedError('write_timeout is not supported')
            self.assertEqual(actual_url,url)
            self.assertTrue(kwargs.get('do_not_open'))
            return port
        serial=SimpleNamespace(serial_for_url=factory,SerialException=OSError)
        with patch.dict(sys.modules,{'serial':serial}), patch.object(sys,'argv',
             ['client','--port',url,'--wav','fixture.wav','--rate','24000']), \
             patch.object(self.client.signal,'signal'),patch.object(self.client,'stream_audio'):
            self.client.main()
        self.assertTrue(port.opened)

    def test_download_rejects_incomplete_http_content(self):
        class Response:
            url='https://example.invalid/audio'
            headers={'Content-Length':'10','Content-Type':'audio/mpeg'}
            reads=0
            def __enter__(self): return self
            def __exit__(self,*_args): pass
            def read(self,_count):
                self.reads+=1
                return b'x' if self.reads==1 else b''
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(self.prepare.urllib.request,'urlopen',return_value=Response()):
                with self.assertRaises(ValueError):
                    self.prepare.download(Response.url,Path(directory)/'audio')

    def test_prepare_cloud_ffmpeg_fixture(self):
        p=self.prepare
        with tempfile.TemporaryDirectory() as directory:
            base=Path(directory); source=base/'fixture.wav'
            subprocess.run(['ffmpeg','-hide_banner','-loglevel','error','-f','lavfi','-i',
                            'sine=frequency=440:duration=3:sample_rate=44100','-ac','2',str(source)],check=True)
            def fixture_download(url,path,limit=20*1024*1024):
                if url==p.SOURCE_URL: shutil.copyfile(source,path)
                elif url==p.METADATA_URL: path.write_text(json.dumps([{'title':'Carefree','isrc':'USUAN1400037'}]))
                else: path.write_text('isolated test fixture')
                return {'requested_url':url}
            with patch.object(p,'download',side_effect=fixture_download):
                report=p.prepare(base/'output',0,3)
            self.assertEqual(report['outputs']['24000']['samples'],72000)
            self.assertEqual(report['outputs']['48000']['samples'],144000)
            self.assertFalse(report['physical_loudness_equal'])
            self.assertTrue((base/'output'/'ATTRIBUTION.txt').is_file())
            for output in report['outputs'].values():
                self.assertLess(output['true_peak_dbtp'],-0.1)
                self.assertLess(output['sample_peak'],32767)
                self.assertEqual(len(output['sha256']),64)

    def test_gain_shared_peak_bounded(self):
        self.assertEqual(self.prepare.choose_gain(-20, -1, -23, -2), -3)
        self.assertEqual(self.prepare.choose_gain(-30, -1, -23, -2), -1)
        for i, tp in ((float('nan'),-2), (float('-inf'),-2),(-20,float('inf'))):
            with self.assertRaises(ValueError): self.prepare.choose_gain(i,tp,-23,-2)

if __name__ == '__main__': unittest.main()

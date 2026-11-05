"""Cloud-only protocol and authenticated asset tests; fake SDKs never prove accuracy."""
import array
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import socket
import struct
import tempfile
import unittest

spec = importlib.util.find_spec('product_sdk_worker')
w = __import__('product_sdk_worker') if spec else None
SHA = 'a' * 64


class WorkerTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(w, 'research SDK worker is not implemented')

    def header(self, **kw):
        h = dict(version=1, id=1, session='s', scope='q', generation=1,
                 start=0, end=32000, model_sha256=SHA,
                 sample_rate=16000, pcm_bytes=64000)
        h.update(kw)
        return h

    def frame(self, h, payload=b''):
        raw = json.dumps(h).encode()
        return struct.pack('>I', len(raw)) + raw + payload

    def test_strict_json_rejects_duplicates_null_trailing(self):
        for value in [b'{"a":1,"a":2}', b'{"a":null}', b'{"a":1} {}', b'[]']:
            with self.subTest(value=value), self.assertRaises(w.Invalid):
                w.strict_json(value)

    def test_request_limits_and_numeric_types(self):
        for kw in [dict(id=True), dict(end=128001, pcm_bytes=256002),
                   dict(id=0), dict(generation=0), dict(session='../secret'),
                   dict(sample_rate=8000), dict(pcm_bytes=1),
                   dict(end=0), dict(model_sha256='b'*64), dict(extra=1)]:
            with self.subTest(kw=kw), self.assertRaises(w.Invalid):
                w.read_request(io.BytesIO(self.frame(self.header(**kw))), SHA)
        h, p = w.read_request(io.BytesIO(self.frame(self.header(), b'\0'*64000)), SHA)
        self.assertEqual(len(p), 64000)
        self.assertIsInstance(p, bytearray)
        w.clear(p)
        self.assertEqual(set(p), {0})

    def test_owned_pcm_read_uses_mutable_readinto(self):
        class MutableReader(io.BytesIO):
            def read(self, size=-1):
                raise AssertionError('immutable PCM copy')
            def readinto(self, value):
                return super().readinto(value[:7])
        pcm = w.exact_read(MutableReader(b'abcdefghij'), 10)
        self.assertEqual(pcm, b'abcdefghij')
        w.clear(pcm)
        self.assertFalse(any(pcm))

    def test_headers_are_bounded_and_truncation_fails(self):
        for raw in [b'', struct.pack('>I', 4097), struct.pack('>I', 0),
                    struct.pack('>I', 3)+b'{}', self.frame(self.header(), b'\0')]:
            with self.subTest(size=len(raw)), self.assertRaises(w.Invalid):
                w.read_request(io.BytesIO(raw), SHA)

    def test_vectors_owned_finite_nonzero_dimension(self):
        for v in [[0., 0.], [float('nan'), 1.], [float('inf'), 1.], [1.], [True, 1.], ['1', 2.], [1e40, 1.]]:
            with self.subTest(v=v), self.assertRaises(w.Invalid):
                w.vector(v, 2)
        v = w.vector([1., 2.], 2)
        self.assertEqual(list(v), [1., 2.])
        w.clear(v)
        self.assertEqual(list(v), [0., 0.])

    def test_ready_result_exact_fields_and_unavailable(self):
        f = io.BytesIO()
        w.write_ready(f, SHA, 128)
        h = w.read_header(io.BytesIO(f.getvalue()))
        self.assertEqual(set(h), w.RESPONSE_KEYS)
        self.assertEqual(h['kind'], 'ready')
        self.assertEqual(h['vector_bytes'], 0)
        f = io.BytesIO()
        w.write_result(f, self.header(), 128, None, 9)
        h = w.read_header(io.BytesIO(f.getvalue()))
        self.assertEqual(h['dimension'], 128)
        self.assertEqual(h['code'], 'unavailable')
        self.assertEqual(h['vector_bytes'], 0)
        self.assertEqual(len(f.getvalue()), 4+len(json.dumps(h,separators=(',',':')).encode()))

    def test_sequence_unavailable_reusable_cleanup_and_failure(self):
        incoming = self.frame(self.header(id=1), b'\1'*64000)
        incoming += self.frame(self.header(id=3), b'\2'*64000)
        incoming += self.frame(self.header(id=3), b'\3'*64000)
        owned = []
        def extract(p):
            owned.append(p)
            return None if len(owned)==1 else array.array('f', [1.,2.])
        out = io.BytesIO()
        with self.assertRaises(w.Invalid):
            w.serve(io.BytesIO(incoming), out, SHA, 2, extract)
        self.assertEqual(len(owned), 2)
        self.assertTrue(all(not any(p) for p in owned))
        r = io.BytesIO(out.getvalue())
        self.assertEqual(w.read_header(r)['code'], 'ok')
        self.assertEqual(w.read_header(r)['code'], 'unavailable')
        self.assertEqual(w.read_header(r)['code'], 'ok')
        self.assertEqual(len(r.read()), 8)

    def test_exception_also_clears_pcm_and_does_not_leak(self):
        owned=[]
        def bad(p):
            owned.append(p)
            raise RuntimeError('private transcript secret')
        out=io.BytesIO()
        with self.assertRaises(RuntimeError):
            w.serve(io.BytesIO(self.frame(self.header(), b'\1'*64000)),out,SHA,2,bad)
        self.assertFalse(any(owned[0]))
        self.assertNotIn(b'private',out.getvalue())

    def test_partial_writes_complete_frame_and_invalid_dimension_rejected(self):
        class Partial(io.BytesIO):
            def write(self, data):
                return super().write(data[:3])
        f = Partial()
        w.write_ready(f, SHA, 2)
        self.assertEqual(w.read_header(io.BytesIO(f.getvalue()))['dimension'], 2)
        with self.assertRaises(w.Invalid):
            w.write_result(io.BytesIO(), self.header(), 0, None, 0)

    def test_stop_signal_unwinds_owned_cleanup(self):
        pcm = bytearray(b'private')
        with self.assertRaises(SystemExit):
            try:
                w.stop_signal(15, None)
            finally:
                w.clear(pcm)
        self.assertFalse(any(pcm))

    def test_offline_audit_blocks_network(self):
        for event, args in [('socket.getaddrinfo', ('example.com', 443)),
                            ('socket.connect', (type('S', (), {'family': socket.AF_INET})(), ('example.com',443)))]:
            with self.subTest(event=event), self.assertRaises(w.Invalid):
                w.offline_audit(event, args)
        w.offline_audit('socket.connect', (type('S', (), {'family': socket.AF_UNIX})(), '/private/socket'))

    def test_bundle_auth_canonical_paths_hash_roles_metadata(self):
        with tempfile.TemporaryDirectory() as td:
            root=Path(td); asset=root/'model'; asset.write_bytes(b'asset')
            digest=hashlib.sha256(b'asset').hexdigest()
            metadata=dict(sdk='fake',sdk_version='1',dimension=2,source_revision='r',frontend='official')
            profile=dict(metadata=metadata,assets={'model':digest})
            lock={'schema_version':1,'profiles':{'fixture':profile}}
            bundle=dict(schema_version=1,profile='fixture',assets=[dict(role='model',path=str(asset),sha256=digest)],metadata=metadata)
            bpath=root/'bundle.json'
            def load(value=bundle, path=bpath, sha=None):
                raw=json.dumps(value).encode(); bpath.write_bytes(raw)
                return w.load_bundle(str(path),sha or hashlib.sha256(raw).hexdigest(),lock)
            result=load(); self.assertEqual(result['paths']['model'],str(asset))
            bad=[]
            bad.append(dict(bundle,extra=True)); bad.append(dict(bundle,metadata=dict(metadata,dimension=3)))
            bad.append(dict(bundle,schema_version=True)); bad.append(dict(bundle,assets=[]))
            bad.append(dict(bundle,metadata=dict(metadata,source_kind='override')))
            bad.append(dict(bundle,assets=[dict(bundle['assets'][0],extra=1)]))
            bad.append(dict(bundle,assets=[dict(bundle['assets'][0],role='unknown')]))
            bad.append(dict(bundle,profile='unknown')); bad.append(dict(bundle,assets=bundle['assets']*2))
            bad.append(dict(bundle,assets=[dict(bundle['assets'][0],sha256='f'*64)]))
            bad.append(dict(bundle,assets=[dict(bundle['assets'][0],path=str(root/'x/../model'))]))
            link=root/'link'; link.symlink_to(asset)
            bad.append(dict(bundle,assets=[dict(bundle['assets'][0],path=str(link))]))
            for value in bad:
                with self.subTest(value=value),self.assertRaises(w.Invalid):load(value)
            with self.assertRaises(w.Invalid):load(sha='f'*64)
            with self.assertRaises(w.Invalid):load(path=root/'x/../bundle.json')
            raw=b' '*65537;bpath.write_bytes(raw)
            with self.assertRaises(w.Invalid):w.load_bundle(str(bpath),hashlib.sha256(raw).hexdigest(),lock)
            asset.write_bytes(b'tampered')
            with self.assertRaises(w.Invalid):load()


if __name__ == '__main__':
    unittest.main()

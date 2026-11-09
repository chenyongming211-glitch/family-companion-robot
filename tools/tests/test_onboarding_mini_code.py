"""Cloud-only synthetic tests; never sends a request to WeChat."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import ssl
import stat
import struct
import tempfile
import unittest
from unittest import mock
import zlib

SPEC = importlib.util.spec_from_file_location("onboarding_mini_code", Path(__file__).parents[1] / "onboarding_mini_code.py")
code = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(code)
CLAIM = "abcdef0123456789" * 2
SECRET = "1234567890abcdef" * 2
TOKEN = "synthetic_private_access_token"
KEY = "synthetic_device_key_do_not_print"


def png():
    def chunk(kind, data):
        return struct.pack('>I', len(data)) + kind + data + struct.pack('>I', zlib.crc32(kind + data))
    return b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', 1, 1, 8, 2, 0, 0, 0)) + chunk(b'IDAT', zlib.compress(b'\x00\x00\x00\x00')) + chunk(b'IEND', b'')


class Response:
    def __init__(self, status=200, content_type='application/json', data=None):
        self.status, self.content_type = status, content_type
        self.data = json.dumps({'access_token': TOKEN, 'expires_in': 7200}).encode() if data is None else data
        self.read_sizes = []
    def getheader(self, name, default=None):
        return self.content_type if name.lower() == 'content-type' else default
    def read(self, size):
        self.read_sizes.append(size)
        return self.data[:size]


class Transport:
    def __init__(self, responses=None, failure=None):
        self.responses = responses or [Response(), Response(content_type='image/png', data=png())]
        self.requests, self.connections, self.closed = [], [], 0
        self.failure = failure
    def __call__(self, host, **kwargs):
        self.connections.append((host, kwargs))
        owner = self
        class Connection:
            def request(self, method, path, body=None, headers=None):
                owner.requests.append((method, path, json.loads(body), headers))
                if owner.failure:
                    raise owner.failure
            def getresponse(self):
                return owner.responses.pop(0)
            def close(self):
                owner.closed += 1
        return Connection()


class MiniCodeTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.bundle = self.root / 'factory.json'
        self.config = self.root / 'wechat.json'
        self.out = self.root / 'device.png'
        self.bundle_data = {'schema_version': 1, 'device_id': 'dev_' + 'a' * 32, 'mac': 'AA:BB:CC:DD:EE:FF', 'serial': 'unit-001', 'model': '微雪 ESP32-S3', 'device_key': KEY, 'claim_code': CLAIM, 'bootstrap_url': 'https://robot.songguoxue.com/api/device/bootstrap', 'voice_url': 'wss://robot.songguoxue.com/xiaozhi/v1/'}
        self.write(self.bundle, self.bundle_data)
        self.write(self.config, {'appid': 'wxb3f1745f8ed1fa1d', 'secret': SECRET})
    def write(self, path, value):
        path.write_text(json.dumps(value))
        path.chmod(0o600)
    def run_generator(self, transport=None, environment='trial'):
        return code.generate(self.bundle, self.config, environment, self.out, connection_factory=transport or Transport())
    def reject(self, transport=None, environment='trial'):
        with self.assertRaises(code.MiniCodeError) as caught:
            self.run_generator(transport, environment)
        for secret in (CLAIM, SECRET, TOKEN, KEY):
            self.assertNotIn(secret, repr(caught.exception))
        self.assertFalse(self.out.exists())
    def test_requests_are_fixed_and_explicit_for_every_environment(self):
        for environment in ('develop', 'trial', 'release'):
            with self.subTest(environment=environment):
                transport = Transport()
                self.run_generator(transport, environment)
                self.assertTrue(self.out.exists(), "successful generation must create an image")
                self.assertEqual(self.out.read_bytes(), png())
                self.assertEqual(stat.S_IMODE(self.out.stat().st_mode), 0o600)
                self.assertEqual([c[0] for c in transport.connections], ['api.weixin.qq.com'] * 2)
                for _, options in transport.connections:
                    self.assertEqual(options['context'].verify_mode, ssl.CERT_REQUIRED)
                    self.assertTrue(options['context'].check_hostname)
                    self.assertLessEqual(options['timeout'], 15)
                self.assertEqual(transport.requests[0][:3], ('POST', '/cgi-bin/stable_token', {'grant_type': 'client_credential', 'appid': 'wxb3f1745f8ed1fa1d', 'secret': SECRET, 'force_refresh': False}))
                self.assertEqual(transport.requests[1][:3], ('POST', '/wxa/getwxacodeunlimit?access_token=' + TOKEN, {'scene': CLAIM, 'page': 'pages/index/index', 'env_version': environment, 'check_path': environment == 'release'}))
                self.assertNotIn(KEY, repr(transport.requests))
                self.assertEqual(transport.closed, 2)
                self.out.unlink()
    def test_rejects_private_file_mode_symlink_directory_and_oversize(self):
        for kind in ('mode', 'symlink', 'directory', 'oversize', 'hardlink'):
            with self.subTest(kind=kind):
                original = self.bundle.read_bytes()
                if kind == 'mode': self.bundle.chmod(0o644)
                elif kind == 'symlink':
                    target = self.root / 'target'; target.write_bytes(original); target.chmod(0o600)
                    self.bundle.unlink(); self.bundle.symlink_to(target)
                elif kind == 'directory': self.bundle.unlink(); self.bundle.mkdir()
                elif kind == 'oversize': self.bundle.write_bytes(b' ' * 16385)
                elif kind == 'hardlink': os.link(self.bundle, self.root / 'extra')
                transport = Transport()
                self.reject(transport)
                self.assertEqual(transport.requests, [])
                if self.bundle.is_dir(): self.bundle.rmdir()
                else: self.bundle.unlink()
                for extra in ('target', 'extra'):
                    (self.root / extra).unlink(missing_ok=True)
                self.bundle.write_bytes(original); self.bundle.chmod(0o600)
    def test_rejects_duplicate_unknown_keys_and_invalid_values(self):
        original = self.config.read_bytes()
        for raw in (b'{"appid":"wxb3f1745f8ed1fa1d","appid":"wxb3f1745f8ed1fa1d","secret":"' + SECRET.encode() + b'"}', json.dumps({'appid': 'wxb3f1745f8ed1fa1d', 'secret': SECRET, 'api_host': 'evil.invalid'}).encode(), json.dumps({'appid': 'wxinvalid', 'secret': SECRET}).encode(), json.dumps({'appid': 'wxb3f1745f8ed1fa1d', 'secret': 'x' * 32}).encode(), b'[]', b'\xff'):
            self.config.write_bytes(raw); self.reject()
        self.config.write_bytes(original)
        for claim in ('A' * 32, 'a' * 31, 123, None):
            self.write(self.bundle, {**self.bundle_data, 'claim_code': claim}); self.reject()
        self.write(self.bundle, {**self.bundle_data, 'override': 'host'}); self.reject()
    def test_invalid_environment_and_output_are_rejected_before_network(self):
        self.reject(environment='latest')
        self.root.chmod(0o755)
        transport = Transport(); self.reject(transport); self.assertEqual(transport.requests, [])
        self.root.chmod(0o700)
        self.out.symlink_to(self.bundle)
        with self.assertRaises(code.MiniCodeError): self.run_generator(Transport())
        self.assertEqual(json.loads(self.bundle.read_text()), self.bundle_data)
        self.out.unlink(); self.out.write_bytes(b'existing')
        transport = Transport()
        with self.assertRaises(code.MiniCodeError): self.run_generator(transport)
        self.assertEqual(self.out.read_bytes(), b'existing'); self.assertEqual(transport.requests, [])
    def test_redirect_http_json_content_type_and_image_errors_leave_no_output(self):
        responses = [Response(status=302), Response(status=403, data=(TOKEN + SECRET).encode()), Response(data=json.dumps({'errcode': 40001, 'errmsg': TOKEN + CLAIM}).encode()), Response(content_type='image/png', data=b'{"errcode":1}'), Response(content_type='image/jpeg', data=png()), Response(content_type='text/plain', data=png()), Response(content_type='image/png', data=png() + b'x' * 1048576)]
        for response in responses:
            with self.subTest(status=response.status, content_type=response.content_type):
                self.reject(Transport([Response(), response]))
    def test_rejects_missing_oversized_or_malformed_token(self):
        for token in ('', 'x' * 513, 'token?leak=' + SECRET, 42):
            self.reject(Transport([Response(data=json.dumps({'access_token': token, 'expires_in': 7200}).encode())]))
        for raw in (b'{"access_token":"a","access_token":"b"}', b'[]', b'x' * 16385):
            self.reject(Transport([Response(data=raw)]))
    def test_network_and_write_errors_are_sanitized_and_cleaned(self):
        transport = Transport(failure=OSError('https://api.weixin.qq.com/' + TOKEN + SECRET + CLAIM))
        self.reject(transport)
        self.assertEqual(transport.closed, 1)
        with mock.patch.object(code.os, 'fsync', side_effect=OSError(TOKEN + SECRET)):
            self.reject()
    def test_cli_does_not_echo_secrets_from_errors_or_arguments(self):
        stdout, stderr = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            result = code.main(['--bundle', str(self.bundle), '--wechat-config', str(self.config), '--environment', 'trial', '--output', str(self.out), '--api-host', SECRET])
        self.assertEqual(result, 1)
        self.assertEqual(stdout.getvalue(), '')
        self.assertEqual(stderr.getvalue(), 'Mini-program code generation failed.\n')

    def test_accepts_all_factory_serial_forms(self):
        self.write(self.bundle, {**self.bundle_data, 'serial': '_UNIT-001'})
        try:
            self.run_generator()
        except code.MiniCodeError:
            self.fail('a factory-valid serial must generate an image')
        self.assertEqual(self.out.read_bytes(), png())

    def test_rejects_nonfactory_serial(self):
        self.write(self.bundle, {**self.bundle_data, 'serial': 'unit.001'})
        self.reject()

    def test_image_creation_cannot_overwrite_a_racing_file(self):
        transport = Transport()
        factory = transport.__call__
        def racing_factory(host, **kwargs):
            self.out.write_bytes(b'created by another operator')
            return factory(host, **kwargs)
        with self.assertRaises(code.MiniCodeError):
            self.run_generator(racing_factory)
        self.assertEqual(self.out.read_bytes(), b'created by another operator')

    def test_parent_symlink_and_fifo_input_never_reach_transport(self):
        alias = self.root / 'alias'
        alias.symlink_to(self.root, target_is_directory=True)
        transport = Transport()
        with self.assertRaises(code.MiniCodeError):
            code.generate(alias / 'factory.json', self.config, 'trial', self.out, connection_factory=transport)
        self.bundle.unlink()
        os.mkfifo(self.bundle, 0o600)
        self.reject(transport)
        self.assertEqual(transport.requests, [])

    def test_bounded_reads_and_directory_durability(self):
        token_response, image_response = Response(), Response(content_type='image/png', data=png())
        original_fsync = os.fsync
        synced = []
        def fsync(fd):
            synced.append(stat.S_ISDIR(os.fstat(fd).st_mode))
            return original_fsync(fd)
        with mock.patch.object(code.os, 'fsync', side_effect=fsync):
            self.run_generator(Transport([token_response, image_response]))
        self.assertEqual(token_response.read_sizes, [16385])
        self.assertEqual(image_response.read_sizes, [1048577])
        self.assertEqual(synced, [False, True])

    def test_cli_upstream_error_does_not_echo_private_contents(self):
        stdout, stderr = io.StringIO(), io.StringIO()
        transport = Transport(failure=OSError(TOKEN + SECRET + CLAIM + KEY))
        with mock.patch.object(code.http.client, 'HTTPSConnection', transport), contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            result = code.main(['--bundle', str(self.bundle), '--wechat-config', str(self.config), '--environment', 'trial', '--output', str(self.out)])
        self.assertEqual(result, 1)
        self.assertEqual(stdout.getvalue(), '')
        self.assertEqual(stderr.getvalue(), 'Mini-program code generation failed.\n')
        self.assertFalse(self.out.exists())

    def test_cancellation_during_output_is_cleaned_and_preserved(self):
        original_write, original_fsync = os.write, os.fsync
        for exception_type in (KeyboardInterrupt, SystemExit):
            for stage in ('partial_write', 'file_fsync', 'directory_fsync'):
                with self.subTest(exception=exception_type.__name__, stage=stage):
                    interruption = exception_type('synthetic cancellation')
                    interrupted = False
                    def write(fd, data):
                        nonlocal interrupted
                        if stage == 'partial_write' and not interrupted:
                            interrupted = True
                            original_write(fd, data[:8])
                            raise interruption
                        return original_write(fd, data)
                    def fsync(fd):
                        nonlocal interrupted
                        directory = stat.S_ISDIR(os.fstat(fd).st_mode)
                        if not interrupted and ((stage == 'file_fsync' and not directory) or (stage == 'directory_fsync' and directory)):
                            interrupted = True
                            raise interruption
                        return original_fsync(fd)
                    try:
                        with mock.patch.object(code.os, 'write', side_effect=write), mock.patch.object(code.os, 'fsync', side_effect=fsync):
                            with self.assertRaises(exception_type) as caught:
                                self.run_generator()
                        self.assertIs(caught.exception, interruption)
                        self.assertFalse(self.out.exists(), 'cancellation must remove the final pathname')
                    finally:
                        self.out.unlink(missing_ok=True)

    def test_cli_keyboard_interrupt_is_generic_and_returns_130(self):
        stdout, stderr = io.StringIO(), io.StringIO()
        transport = Transport()
        with mock.patch.object(code.http.client, 'HTTPSConnection', transport), mock.patch.object(code.os, 'write', side_effect=KeyboardInterrupt(TOKEN + SECRET + CLAIM + KEY)), contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            try:
                result = code.main(['--bundle', str(self.bundle), '--wechat-config', str(self.config), '--environment', 'trial', '--output', str(self.out)])
            except KeyboardInterrupt:
                self.fail('CLI cancellation must return a sanitized status without a traceback')
        self.assertEqual(result, 130)
        self.assertEqual(stdout.getvalue(), '')
        self.assertEqual(stderr.getvalue(), 'Mini-program code generation cancelled.\n')
        self.assertFalse(self.out.exists())

if __name__ == '__main__':
    unittest.main()

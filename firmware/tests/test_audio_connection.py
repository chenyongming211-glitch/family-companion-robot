"""Execute verbatim prepared protocol methods with platform-only host doubles."""
import hashlib
import importlib.util
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class AudioConnectionTests(unittest.TestCase):
    def test_actual_prepared_protocol_audio_and_hello_failures(self):
        cjson = Path(os.environ.get('FAMILY_CONTROL_CJSON_DIR', str(ROOT / '.cache/control-cjson')))
        for name, digest in {
            'cJSON.c': '607e756460fa0de37d20a7a9181f2de29c97bfb7ce5a0e6c2f548243836cd852',
            'cJSON.h': '25b0145150d500498e4d209cec69c18c42cf818bffcc54690be3b895a2a16dee',
        }.items():
            self.assertTrue((cjson / name).is_file(), 'configure the existing FAMILY_CONTROL_CJSON_DIR fixture')
            self.assertEqual(hashlib.sha256((cjson / name).read_bytes()).hexdigest(), digest)
        import test_realtime_default as defaults
        defaults.RealtimeDefaultTests.setUpClass.__func__(self.__class__)
        spec = importlib.util.spec_from_file_location(
            'connection_preparation', ROOT / 'tools/prepare_connection_diagnostics.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            tree = Path(temporary)
            for relative in ['main/application.cc', 'main/protocols/websocket_protocol.cc',
                             'main/protocols/websocket_protocol.h', 'main/audio/audio_codec.h',
                             'main/audio/playback_websocket.inc']:
                target = tree / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(self.source / relative, target)
            module.prepare_connection_diagnostics(tree)
            protocol = (tree / 'main/protocols/websocket_protocol.cc').read_text()
            playback = (tree / 'main/audio/playback_websocket.inc').read_text()
            signatures = ['bool WebsocketProtocol::OpenAudioChannel()',
                          'bool WebsocketProtocol::SendText(',
                          'std::string WebsocketProtocol::GetHelloMessage()']
            methods = '\n'.join(defaults.method(protocol, s) for s in signatures)
            methods += '\n' + '\n'.join(defaults.method(playback, s) for s in [
                'bool WebsocketProtocol::ResetPlaybackConnection(',
                'void WebsocketProtocol::ParseServerHello(',
                'void WebsocketProtocol::NotifyPlaybackClosed(',
                'uint32_t WebsocketProtocol::AudioChannelEpoch()'])
            if 'void WebsocketProtocol::RejectServerHelloLocked()' in playback:
                methods += '\n' + defaults.method(playback, 'void WebsocketProtocol::RejectServerHelloLocked()')
            fixture = (ROOT / 'tests/audio_connection_host.cc').read_text()
            header = (tree / 'main/protocols/websocket_protocol.h').read_text()
            constants = '\n'.join(line for line in header.splitlines()
                                  if line.startswith('#define WEBSOCKET_PROTOCOL_SERVER_HELLO'))
            codec_header = (tree / 'main/audio/audio_codec.h').read_text()
            codec_api = ('virtual bool PrepareAudioConnection() { return true; }'
                         if 'virtual bool PrepareAudioConnection() { return true; }' in codec_header else '')
            source = tree / 'protocol.cc'
            source.write_text(fixture.replace('// EVENT_CONSTANTS', constants)
                              .replace('// CODEC_API', codec_api)
                              .replace('// PRODUCTION_PROTOCOL', methods))
            binary = tree / 'protocol'
            obj = tree / 'cjson.o'
            result = subprocess.run(['cc', '-std=c11', '-fsanitize=address,undefined',
                                     '-I', str(cjson), '-c', str(cjson / 'cJSON.c'), '-o', str(obj)],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            result = subprocess.run([
                'c++', '-std=c++23', '-pthread', '-Wall', '-Wextra', '-Werror',
                '-Wno-unused-function', '-Wno-missing-field-initializers',
                '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                '-I', str(ROOT / 'tests/connection_trace_stubs'),
                '-I', str(ROOT / 'overlays/onboarding'),
                '-I', str(cjson), str(source), str(obj), '-o', str(binary)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            env = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                       UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                env['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            result = subprocess.run([str(binary)], env=env, capture_output=True,
                                    text=True, timeout=20)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn(codec_api, codec_header)
            self.assertTrue(codec_api, 'base codec must provide the compatible virtual API')


if __name__ == '__main__':
    unittest.main()

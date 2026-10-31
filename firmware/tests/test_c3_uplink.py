"""Execute the production C3 uplink gate with stale/concurrent sends."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
import test_realtime_default as defaults

FIRMWARE = Path(__file__).resolve().parents[1]


class C3UplinkTests(unittest.TestCase):
    def test_product_c3_never_falls_back_to_legacy_mqtt(self):
        class SourceFixture(unittest.TestCase):
            pass
        defaults.RealtimeDefaultTests.setUpClass.__func__(SourceFixture)
        self.addCleanup(SourceFixture.doClassCleanups)
        app = (SourceFixture.source/'main/application.cc').read_text()
        body = defaults.method(app, 'void Application::InitializeProtocol()')
        selection = body.split('display->SetStatus(Lang::Strings::LOADING_PROTOCOL);', 1)[1].split(
            '    protocol_->OnConnected', 1)[0]
        program = '''#include <memory>
#include <cassert>
#include <initializer_list>
#define ESP_LOGW(...) ((void)0)
struct Base { virtual ~Base()=default; int kind=0; };
struct MqttProtocol:Base {};
struct WebsocketProtocol:Base { WebsocketProtocol(){kind=1;} };
struct Ota { bool mqtt=false,ws=false; bool HasMqttConfig(){return mqtt;} bool HasWebsocketConfig(){return ws;} };
struct Application { Ota ota,*ota_=&ota; std::unique_ptr<Base> protocol_; void Choose(){
'''+selection+'''}};
int main(){for(bool mqtt:{false,true})for(bool ws:{false,true}) {
 Application a;a.ota.mqtt=mqtt;a.ota.ws=ws;a.Choose();
 assert(a.protocol_->kind==(CONFIG_XMINI_C3_BUTTON_ENTRY?1:(!mqtt&&ws?1:0)));
}}
'''
        for enabled in (1, 0):
            with self.subTest(enabled=enabled), tempfile.TemporaryDirectory() as temporary:
                work=Path(temporary); source=work/'transport.cc'; binary=work/'transport'
                source.write_text(program)
                compiled=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror','-pedantic',
                    '-O2',f'-DCONFIG_XMINI_C3_BUTTON_ENTRY={enabled}',str(source),'-o',str(binary)],
                    capture_output=True,text=True)
                self.assertEqual(compiled.returncode,0,compiled.stderr)
                executed=subprocess.run([str(binary)],capture_output=True,text=True,timeout=20)
                self.assertEqual(executed.returncode,0,executed.stdout+executed.stderr)

    def test_coalesced_playback_states_restore_closed_capture(self):
        class SourceFixture(unittest.TestCase):
            pass
        defaults.RealtimeDefaultTests.setUpClass.__func__(SourceFixture)
        self.addCleanup(SourceFixture.doClassCleanups)
        app = (SourceFixture.source/'main/application.cc').read_text()
        playback = (SourceFixture.source/'main/audio/playback_application.inc').read_text()
        bodies = [defaults.method(app, signature) for signature in [
            'bool Application::SetDeviceState(', 'void Application::HandleStateChangedEvent()',
            'void Application::StartListeningAudio()', 'void Application::ConfigureWakeWordForListening()']]
        bodies.extend(defaults.method(playback, signature) for signature in [
            'void Application::SchedulePlaybackStart(', 'void Application::SchedulePlaybackStop('])
        program = (FIRMWARE/'tests/c3_coalesced_host.cc').read_text().replace(
            '// PRODUCTION_APPLICATION_METHODS', '\n'.join(bodies))
        for flags in (['-O2'], ['-fsanitize=address,undefined', '-fno-sanitize-recover=all']):
            with self.subTest(flags=flags), tempfile.TemporaryDirectory() as temporary:
                work = Path(temporary); source = work/'coalesced.cc'; binary = work/'coalesced'
                source.write_text(program)
                compiled = subprocess.run([
                    'c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror',
                    '-pedantic', '-Wno-unused-parameter', *flags, '-DCONFIG_XMINI_C3_BUTTON_ENTRY=1',
                    '-I', str(FIRMWARE/'overlays/main/audio'), str(source), '-o', str(binary)],
                    capture_output=True, text=True)
                self.assertEqual(compiled.returncode, 0, compiled.stderr)
                environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                                   UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if '-fsanitize=address,undefined' in flags and os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                executed = subprocess.run([str(binary)], env=environment, capture_output=True,
                                          text=True, timeout=20)
                self.assertEqual(executed.returncode, 0, executed.stdout+executed.stderr)

    def test_application_reopen_cannot_override_ordered_receive_closure(self):
        class SourceFixture(unittest.TestCase):
            pass
        defaults.RealtimeDefaultTests.setUpClass.__func__(SourceFixture)
        self.addCleanup(SourceFixture.doClassCleanups)
        app = (SourceFixture.source/'main/application.cc').read_text()
        body = defaults.method(app, 'void Application::StartListeningAudio()')
        program = (FIRMWARE/'tests/c3_listen_host.cc').read_text().replace(
            '// PRODUCTION_LISTENING_METHOD', body)
        with tempfile.TemporaryDirectory() as temporary:
            work = Path(temporary); source = work/'listen.cc'; binary = work/'listen'
            source.write_text(program)
            compiled = subprocess.run([
                'c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror',
                '-pedantic', '-O2', '-DCONFIG_XMINI_C3_BUTTON_ENTRY=1',
                '-I', str(FIRMWARE/'overlays/main/audio'), str(source), '-o', str(binary)],
                capture_output=True, text=True)
            self.assertEqual(compiled.returncode, 0, compiled.stderr)
            executed = subprocess.run([str(binary)], capture_output=True, text=True, timeout=20)
            self.assertEqual(executed.returncode, 0, executed.stdout+executed.stderr)

    def test_lite_pcm_buffer_cannot_mix_closed_capture_epochs(self):
        implementation = FIRMWARE / 'overlays/main/audio/c3_lite_uplink.inc'
        self.assertTrue(implementation.is_file(), 'C3 lite capture epoch implementation is missing')
        for flags in (['-O2'], ['-fsanitize=address,undefined', '-fno-sanitize-recover=all']):
            with self.subTest(flags=flags), tempfile.TemporaryDirectory() as temporary:
                binary = Path(temporary) / 'lite'
                compiled = subprocess.run([
                    'c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror',
                    '-pedantic', *flags, '-I', str(implementation.parent),
                    str(FIRMWARE / 'tests/c3_lite_uplink_host.cc'), '-o', str(binary)],
                    capture_output=True, text=True)
                self.assertEqual(compiled.returncode, 0, compiled.stderr)
                environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                                   UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if '-fsanitize=address,undefined' in flags and os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                executed = subprocess.run([str(binary)], env=environment, capture_output=True,
                                          text=True, timeout=20)
                self.assertEqual(executed.returncode, 0, executed.stdout + executed.stderr)

    def test_capture_epochs_and_transport_close_ordering(self):
        header = FIRMWARE / 'overlays/main/audio/button_uplink.h'
        self.assertTrue(header.is_file(), 'C3 button uplink implementation is missing')
        for flags in (['-O2'], ['-fsanitize=address,undefined', '-fno-sanitize-recover=all']):
            with self.subTest(flags=flags), tempfile.TemporaryDirectory() as temporary:
                binary = Path(temporary) / 'uplink'
                result = subprocess.run([
                    'c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror',
                    '-pedantic', *flags, '-I', str(header.parent),
                    str(FIRMWARE / 'tests/c3_uplink_host.cc'), '-o', str(binary)],
                    capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                                   UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if '-fsanitize=address,undefined' in flags and os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                result = subprocess.run([str(binary)], env=environment, capture_output=True,
                                        text=True, timeout=20)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

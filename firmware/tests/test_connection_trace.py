"""Cloud host checks for common S3 connection diagnostics, never a voice client."""
import os
import importlib.util
import hashlib
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ConnectionTraceTests(unittest.TestCase):
    def test_actual_application_reuse_preserves_connection_id(self):
        spec = importlib.util.spec_from_file_location(
            'connection_preparation', ROOT / 'tools/prepare_connection_diagnostics.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        import test_realtime_default as defaults
        defaults.RealtimeDefaultTests.setUpClass.__func__(self.__class__)
        with tempfile.TemporaryDirectory() as temporary:
            tree = Path(temporary)
            for relative in ['main/application.cc', 'main/protocols/websocket_protocol.cc',
                             'main/protocols/websocket_protocol.h']:
                target = tree / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(self.source / relative, target)
            module.prepare_connection_diagnostics(tree)
            application = (tree / 'main/application.cc').read_text()
            protocol = (tree / 'main/protocols/websocket_protocol.cc').read_text()
            signatures = ['bool Application::SetDeviceState(', 'void Application::HandleToggleChatEvent()',
                          'void Application::HandleStartListeningEvent()',
                          'void Application::ContinueOpenAudioChannel(', 'void Application::BeginWakeWordInvoke(',
                          'void Application::ContinueWakeWordInvoke(', 'void Application::SetListeningMode(']
            methods = '\n'.join(defaults.method(application, signature) for signature in signatures)
            # Compile the actual application methods; expand only hardware/service
            # fixtures. The protocol seam executes the actual ID/header statements
            # and counts physical opens, without recreating application decisions.
            fixture = (ROOT / 'tests/s3_connect_capture_host.cc').read_text().split('int main() {', 1)[0]
            fixture = fixture.replace('#define ESP_LOGI(...) ((void)0)', '')
            fixture = fixture.replace('kDeviceStateWifiConfiguring };',
                                      'kDeviceStateWifiConfiguring, kDeviceStateActivating, kDeviceStateAudioTesting };')
            fixture = fixture.replace('    bool voice = false, wake = true, uplink = false;',
                                      '    void EnableAudioTesting(bool) {}\n'
                                      '    bool voice = false, wake = true, uplink = false;')
            fixture = fixture.replace('    int audio_sent = 0, detects = 0;', '''
    int audio_sent = 0, detects = 0, opens = 0, probes = 0;
    bool disconnect_on_recheck = false;
    struct Socket {
        std::string header;
        int headers = 0;
        void SetHeader(const char* key, const char* value) {
            assert(std::string(key) == "X-Robot-Connection-Id");
            header = value; ++headers;
        }
    } socket, *websocket_ = &socket;
    void CloseAudioChannel() { opened = false; }
''')
            fixture = fixture.replace('bool IsAudioChannelOpened() const { return opened; }',
                                      'bool IsAudioChannelOpened() {\n'
                                      '    if (++probes == 2 && disconnect_on_recheck) opened = false;\n'
                                      '    return opened;\n}')
            open_method = defaults.method(protocol, 'bool WebsocketProtocol::OpenAudioChannel()')
            id_and_header = '\n'.join(line for line in open_method.splitlines()
                                      if 'const auto trace_attempt =' in line or
                                      'websocket_->SetHeader("X-Robot-Connection-Id"' in line)
            fixture = fixture.replace('    bool OpenAudioChannel() {',
                                      '    bool OpenAudioChannel() {\n        ++opens;\n' + id_and_header)
            fixture = fixture.replace('    void HandleStateChangedEvent();', '''
    void HandleToggleChatEvent();
    void HandleStartListeningEvent();
    void StopNotification() { SetDeviceState(kDeviceStateIdle); }
    void AbortSpeaking(int) {}
''')
            prefix = '''#include <cstdarg>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>
#define TAG "Host"
constexpr int kAbortReasonNone = 0;
int64_t trace_test_us = 1000000;
std::vector<std::string> trace_logs;
void trace_test_log(const char* tag, const char* format, ...) {
    if (std::strcmp(tag, "ConnectionTrace")) return;
    char line[512]; va_list args; va_start(args, format);
    vsnprintf(line, sizeof(line), format, args); va_end(args);
    trace_logs.emplace_back(line);
}
#include "connection_trace.h"
'''
            main = r'''
static bool has(const char* id, const char* stage, int state = -1) {
    for (const auto& line : trace_logs)
        if (line.find(std::string("id=") + id + " ") != std::string::npos &&
            line.find(std::string("stage=") + stage + " ") != std::string::npos &&
            (state == -1 || line.find("state=" + std::to_string(state) + " ") != std::string::npos))
            return true;
    return false;
}
int main() {
    using namespace ConnectionDiagnostics;
    for (int entry = 0; entry < 3; ++entry) {
        for (bool already_open : {true, false}) {
            for (auto initial_state : {kDeviceStateIdle, kDeviceStateNotifying}) {
                if (entry == 2 && initial_state == kDeviceStateNotifying) continue;
                Board::GetInstance().codec.Reset();
                Application app;
                app.state_machine_.state = initial_state;
                const auto previous = Begin(Stage::Button, kDeviceStateIdle);
                app.transport.opened = already_open;
                app.transport.socket.SetHeader("X-Robot-Connection-Id", previous.id);
                trace_logs.clear();
                const auto invoke = [&] {
                    if (entry == 0) app.HandleToggleChatEvent();
                    else if (entry == 1) app.HandleStartListeningEvent();
                    else app.BeginWakeWordInvoke("synthetic-wake");
                };
                invoke();
                if (!already_open) {
                    const auto pending = Current();
                    assert(std::string(pending.id) != previous.id);
                    assert(has(pending.id, entry == 0 ? "button" : entry == 1 ? "start_button" : "wake",
                               entry == 2 ? kDeviceStateConnecting : kDeviceStateIdle));
                    assert(app.transport.opens == 0 && app.transport.socket.headers == 1);
                }
                if (!already_open && entry < 2) {
                    // Repeated button while a scheduled open is pending must
                    // remain visible and must not replace that attempt's ID.
                    const auto pending = Current();
                    assert(app.GetDeviceState() == kDeviceStateConnecting);
                    trace_logs.clear();
                    invoke();
                    assert(std::string(Current().id) == pending.id);
                    assert(has(pending.id, entry == 0 ? "button" : "start_button", kDeviceStateConnecting));
                    assert(app.scheduled.size() == 1 && app.transport.opens == 0);
                }
                app.Drain();
                const auto active = Current();
                if (already_open) {
                    if (std::string(active.id) != previous.id) {
                        std::fprintf(stderr, "entry=%d idle/open reused socket but rotated application ID\n", entry);
                        return 21;
                    }
                    assert(app.transport.opens == 0 && app.transport.socket.headers == 1);
                    assert(app.transport.probes == (entry == 2 ? 2 : 1));
                    assert(has(previous.id, entry == 0 ? "button" : entry == 1 ? "start_button" : "wake"));
                    // No new connection stage or handshake is permitted on reuse.
                    assert(!has(previous.id, "open") && !has(previous.id, "socket_create"));
                } else {
                    assert(std::string(active.id) != previous.id);
                    assert(app.transport.opens == 1 && app.transport.socket.headers == 2);
                    assert(app.transport.probes == 2);
                }
                assert(app.transport.socket.header == active.id);
                assert(app.GetDeviceState() == kDeviceStateListening);
                assert(has(active.id, "state", kDeviceStateListening));
                assert(has(active.id, "listening", kDeviceStateListening));
                if (entry == 2 || !already_open) assert(has(active.id, "continue"));
            }
        }
    }
    {
        // Establish A through the actual production Open ID/header seam, then
        // disconnect between BeginWake's true probe and ContinueWake's false
        // probe. The existing application decisions must reopen with a new ID.
        Board::GetInstance().codec.Reset();
        Application app;
        const auto old = Begin(Stage::Button, kDeviceStateIdle);
        assert(app.transport.OpenAudioChannel());
        assert(app.transport.socket.header == old.id);
        Board::GetInstance().codec.Reset();
        app.transport.probes = 0;
        app.transport.disconnect_on_recheck = true;
        trace_logs.clear();
        app.BeginWakeWordInvoke("synthetic-racing-wake");
        app.Drain();
        const auto reopened = Current();
        if (std::string(reopened.id) == old.id) {
            std::fputs("wake true->false channel race reopened a physical socket with old ID\n", stderr);
            return 22;
        }
        assert(app.transport.opens == 2 && app.transport.socket.headers == 2);
        assert(app.transport.probes == 2 && app.scheduled.empty());
        assert(app.transport.socket.header == reopened.id);
        assert(has(old.id, "wake") && has(old.id, "continue"));
        assert(has(reopened.id, "state", kDeviceStateListening));
        assert(has(reopened.id, "listening", kDeviceStateListening));
    }
    {
        // The recorder alone also protects direct/repeated physical opens,
        // while consuming an app-prepared pending ID exactly once.
        Recorder recorder;
        const auto first = recorder.ForSocket();
        const auto second = recorder.ForSocket();
        assert(first && second && std::string(first.id) != second.id);
        assert(std::string(recorder.Current().id) == second.id);
        const auto pending = recorder.Begin();
        const auto consumed = recorder.ForSocket();
        assert(std::string(consumed.id) == pending.id);
        const auto subsequent = recorder.ForSocket();
        assert(std::string(subsequent.id) != consumed.id);
        assert(std::string(first.id) != subsequent.id); // old snapshots stay values
    }
    puts("actual application reuse/new-open connection IDs passed");
}
'''
            source = tree / 'reuse.cc'
            source.write_text(prefix + fixture.replace('// PRODUCTION_TRANSITION', methods) + main)
            binary = tree / 'reuse'
            compiled = subprocess.run([
                'c++', '-std=c++20', '-pthread', '-Wall', '-Wextra', '-Werror',
                '-DCONFIG_SEND_WAKE_WORD_DATA=1', '-DCONFIG_USE_DEVICE_AEC=1',
                '-DCONFIG_WAVESHARE_XIAOMAN_REALTIME=1',
                '-I', str(ROOT / 'tests/connection_trace_stubs'),
                '-I', str(ROOT / 'overlays/onboarding'),
                '-I', str(ROOT / 'overlays/main/audio'),
                '-I', str(ROOT / 'overlays/main/boards/m5stack/stopwatch'),
                str(source), '-o', str(binary),
            ], capture_output=True, text=True)
            self.assertEqual(compiled.returncode, 0, compiled.stderr)
            result = subprocess.run([str(binary)], capture_output=True, text=True, timeout=20)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_actual_sdk_handshake_faults_and_reconnect(self):
        fixture = os.environ.get('CONNECTION_NETWORK_COMPONENT')
        if not fixture:
            self.skipTest('cloud SDK component fixture not configured')
        component = Path(fixture)
        pins = {
            'src/web_socket.cc': '978edda0e10368e4effdc17e96d57bffc67b47c98732da5bfaf5295c656b722e',
            'include/web_socket.h': '07880946174351e967191f90b43a59964272aa064132cdfffda7e457205764a1',
            'include/tcp.h': 'd81aee7b684debcce73211bf9dc90284301c2aaa3eaf6b676c534c6523847694',
            'include/network_error.h': '1bea76e3a97406ef1ea9e04d030057c668a1608b8af248b695b14435c57305ce',
        }
        for name, digest in pins.items():
            self.assertEqual(hashlib.sha256((component / name).read_bytes()).hexdigest(), digest, name)
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / 'socket'
            compiled = subprocess.run([
                'c++', '-std=c++23', '-pthread', '-Wall', '-Wextra', '-Werror',
                '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                '-I', str(ROOT / 'tests/connection_socket_stubs'),
                '-I', str(component / 'include'),
                '-I', str(ROOT / 'tests/connection_trace_stubs'),
                '-I', str(ROOT / 'overlays/onboarding'),
                str(ROOT / 'tests/connection_socket_host.cc'),
                str(component / 'src/web_socket.cc'), '-o', str(binary),
            ], capture_output=True, text=True)
            self.assertEqual(compiled.returncode, 0, compiled.stderr)
            environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                               UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            result = subprocess.run([str(binary)], capture_output=True, text=True, env=environment, timeout=20)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        for name, digest in pins.items():
            self.assertEqual(hashlib.sha256((component / name).read_bytes()).hexdigest(), digest, name)

    def test_preparation_instruments_the_actual_application_and_protocol(self):
        tool = ROOT / 'tools/prepare_connection_diagnostics.py'
        self.assertTrue(tool.is_file(), 'missing shared connection observation preparation')
        spec = importlib.util.spec_from_file_location('connection_preparation', tool)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        import test_realtime_default as defaults
        defaults.RealtimeDefaultTests.setUpClass.__func__(self.__class__)
        with tempfile.TemporaryDirectory() as temporary:
            tree = Path(temporary)
            for relative in ['main/application.cc', 'main/protocols/websocket_protocol.cc',
                             'main/protocols/websocket_protocol.h']:
                target = tree / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(self.source / relative, target)
            module.prepare_connection_diagnostics(tree)
            application = (tree / 'main/application.cc').read_text()
            protocol = (tree / 'main/protocols/websocket_protocol.cc').read_text()
            self.assertIn('ConnectionDiagnostics::Observe(ConnectionDiagnostics::Stage::Button', application)
            self.assertIn('ConnectionDiagnostics::Stage::Continue', application)
            self.assertIn('ConnectionDiagnostics::Stage::Listening', application)
            self.assertIn('websocket_->SetHeader("X-Robot-Connection-Id", trace_attempt.id)', protocol)
            self.assertIn('[this, connection, trace_attempt]', protocol)
            self.assertIn('ConnectionDiagnostics::Stage::HelloReceived', protocol)
            self.assertIn('ConnectionDiagnostics::Stage::HelloTimeout', protocol)
            header = (tree / 'main/protocols/websocket_protocol.h').read_text()
            self.assertLess(header.index('connection_trace_network_'), header.index('std::unique_ptr<WebSocket>'))
            # Compile the verbatim instrumented state/open/wake paths with the
            # established DMA/capture fixture, rather than reimplementing them.
            signatures = ['bool Application::SetDeviceState(', 'void Application::HandleStateChangedEvent()',
                          'void Application::ContinueOpenAudioChannel(', 'void Application::BeginWakeWordInvoke(',
                          'void Application::ContinueWakeWordInvoke(', 'void Application::SetListeningMode(']
            methods = '\n'.join(defaults.method(application, signature) for signature in signatures)
            fixture = (ROOT / 'tests/s3_connect_capture_host.cc').read_text().replace('#define ESP_LOGI(...) ((void)0)', '')
            fixture = '#include <cstdint>\n#define TAG "Host"\nint64_t trace_test_us=1000000;\nvoid trace_test_log(const char*,const char*,...){}\n#include "connection_trace.h"\n' + fixture
            source = tree / 'observed.cc'
            source.write_text(fixture.replace('// PRODUCTION_TRANSITION', methods))
            binary = tree / 'observed'
            compiled = subprocess.run([
                'c++', '-std=c++20', '-pthread', '-Wall', '-Wextra', '-Werror',
                '-DCONFIG_SEND_WAKE_WORD_DATA=1', '-DCONFIG_USE_DEVICE_AEC=1',
                '-DCONFIG_WAVESHARE_XIAOMAN_REALTIME=1',
                '-I', str(ROOT / 'tests/connection_trace_stubs'),
                '-I', str(ROOT / 'overlays/onboarding'),
                '-I', str(ROOT / 'overlays/main/audio'),
                '-I', str(ROOT / 'overlays/main/boards/m5stack/stopwatch'),
                str(source), '-o', str(binary),
            ], capture_output=True, text=True)
            self.assertEqual(compiled.returncode, 0, compiled.stderr)
            executed = subprocess.run([str(binary)], capture_output=True, text=True, timeout=20)
            self.assertEqual(executed.returncode, 0, executed.stdout + executed.stderr)

    def test_transport_is_transparent_and_old_callbacks_keep_their_attempt(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / 'trace'
            compiled = subprocess.run([
                'c++', '-std=c++23', '-pthread', '-Wall', '-Wextra', '-Werror',
                '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                '-I', str(ROOT / 'tests/connection_trace_stubs'),
                '-I', str(ROOT / 'overlays/onboarding'),
                str(ROOT / 'tests/connection_trace_host.cc'), '-o', str(binary),
            ], capture_output=True, text=True)
            self.assertEqual(compiled.returncode, 0, compiled.stderr)
            environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                               UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            executed = subprocess.run([str(binary)], env=environment, capture_output=True,
                                      text=True, timeout=20)
            self.assertEqual(executed.returncode, 0, executed.stdout + executed.stderr)


if __name__ == '__main__':
    unittest.main()

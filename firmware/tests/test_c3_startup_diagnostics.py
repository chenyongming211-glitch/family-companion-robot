"""Exercise numeric startup evidence in the actual C3 codec implementation."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
import test_realtime_default as defaults

FIRMWARE=Path(__file__).resolve().parents[1]
BOARD=FIRMWARE/'overlays/main/boards/xmini/c3'

class C3StartupDiagnosticsTests(unittest.TestCase):
    def test_isr_latch_and_fault_graph_never_log_allocate_or_wait(self):
        lifecycle=(BOARD/'family_c3_lifecycle.inc').read_text()
        capture=(BOARD/'family_c3_capture.inc').read_text()
        for source,signature in [(lifecycle,'void FamilyC3Codec::RecordStartupFaultLocked('),
                (lifecycle,'void FamilyC3Codec::FaultLocked('),(capture,'void FamilyC3Codec::ResetCaptureLocked('),
                (capture,'void FamilyC3Codec::PublishReferenceLocked(')]:
            body=defaults.method(source,signature)
            for forbidden in ('ESP_LOG','printf','malloc','heap_caps','new(','vTaskDelay','Semaphore','std::mutex'):
                self.assertNotIn(forbidden,body)
        self.assertIn('startup_fault_.source!=StartupFaultSource::None',defaults.method(lifecycle,'void FamilyC3Codec::RecordStartupFaultLocked('))
        self.assertIn('portENTER_CRITICAL(&mux_)',defaults.method(lifecycle,'family_robot::C3CodecStartupDiagnostics FamilyC3Codec::StartupDiagnostics('))

    def test_first_fault_hardware_mask_and_isr_snapshot(self):
        with tempfile.TemporaryDirectory() as temporary:
            work=Path(temporary)
            for relative in ['codecs/es8311_audio_codec.h','driver/i2s_std.h','esp_heap_caps.h','esp_timer.h']:
                path=work/relative;path.parent.mkdir(parents=True,exist_ok=True);path.write_text('// harness SDK plumbing\n')
            (work/'sdkconfig.h').write_text('#define CONFIG_XMINI_C3_BUTTON_ENTRY 1\n#define CONFIG_XMINI_C3_VOICE_BARGE 1\n#define CONFIG_XMINI_C3_PCM 1\n')
            plumbing=(FIRMWARE/'tests/c3_codec_host.cc').read_text().split('#include "family_c3_codec.h"',1)[0]
            source=work/'startup.cc';source.write_text(plumbing+(FIRMWARE/'tests/c3_startup_diagnostics_host.cc').read_text())
            for mode,flags in [('optimized',['-O2']),('sanitized',['-O2','-g','-fsanitize=address,undefined','-fno-sanitize-recover=all'])]:
                with self.subTest(mode=mode):
                    binary=work/mode
                    compiled=subprocess.run(['c++','-std=c++17','-pthread','-Wall','-Wextra','-Werror','-pedantic','-Wno-unused-function',*flags,
                        '-I',str(work),'-I',str(BOARD),'-I',str(FIRMWARE/'overlays/main/audio'),str(source),'-o',str(binary)],capture_output=True,text=True)
                    self.assertEqual(compiled.returncode,0,compiled.stderr)
                    env=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                    if mode=='sanitized' and os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                    run=subprocess.run([str(binary)],env=env,capture_output=True,text=True,timeout=20)
                    self.assertEqual(run.returncode,0,run.stdout+run.stderr)

"""C3 cache-off contracts. Runtime execution belongs to the cloud runner."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]


class CacheSafeTests(unittest.TestCase):
    def test_span_publication_and_fail_closed_observation(self):
        header = FIRMWARE/'overlays/main/audio/c3_cache_span.h'
        self.assertTrue(header.is_file(), 'bounded cache span observer is missing')
        with tempfile.TemporaryDirectory() as temporary:
            for label, flags in [('O2', ['-O2']), ('asan', ['-O2', '-g',
                    '-fsanitize=address,undefined', '-fno-sanitize-recover=all'])]:
                binary = Path(temporary)/label
                result = subprocess.run(['c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror',
                    '-pedantic', *flags, '-I', str(header.parent),
                    str(FIRMWARE/'tests/c3_cache_span_host.cc'), '-o', str(binary)],
                    text=True, capture_output=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                                   UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                result = subprocess.run([str(binary)], env=environment,
                                        text=True, capture_output=True, timeout=20)
                self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
                print(label+': '+result.stdout.strip())

    def test_native_auditor_is_available(self):
        self.assertTrue((FIRMWARE/'overlays/scripts/family_c3_iram_audit.py').is_file(),
                        'final ELF cache safety gate is missing')

    def test_pcm_uses_iram_without_button_pcm_dependency_cycle(self):
        button = (FIRMWARE/'patches/0010-xmini-c3-button-entry.patch').read_text()
        pcm = (FIRMWARE/'patches/0015-c3-bounded-pcm.patch').read_text()
        self.assertNotIn('depends on !USE_AUDIO_PROCESSOR && !I2S_ISR_IRAM_SAFE', button)
        self.assertNotIn('I2S_ISR_IRAM_SAFE || XMINI_C3_PCM', button)
        self.assertIn('"CONFIG_I2S_ISR_IRAM_SAFE=y"', pcm)


if __name__ == '__main__':
    unittest.main()

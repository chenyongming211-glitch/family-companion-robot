"""Pure fixed PCM pool contracts, compiled and run in the cloud host suite."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]


class C3PcmBufferTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temp.cleanup)
        cls.binaries = {}
        for mode, flags in [('optimized', ['-O2']), ('sanitized', ['-O1', '-g',
                '-fsanitize=address,undefined', '-fno-sanitize-recover=all'])]:
            binary = Path(cls.temp.name) / mode
            result = subprocess.run(['c++', '-std=c++17', '-Wall', '-Wextra', '-Werror',
                '-pedantic', *flags, '-I', str(FIRMWARE / 'overlays/main/audio'),
                str(FIRMWARE / 'tests/c3_pcm_buffer_host.cc'), '-o', str(binary)],
                capture_output=True, text=True)
            if result.returncode:
                raise AssertionError(result.stderr)
            cls.binaries[mode] = binary

    def run_case(self, case):
        for mode, binary in self.binaries.items():
            with self.subTest(mode=mode):
                env = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                           UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if mode == 'sanitized' and os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    env['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                result = subprocess.run([str(binary), case], capture_output=True,
                                        text=True, env=env, timeout=10)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_aec_tails_order_and_transactional_full(self): self.run_case('input')
    def test_epoch_adoption_reset_and_stale_rejection(self): self.run_case('epoch')
    def test_shapes_and_empty_results(self): self.run_case('shapes')
    def test_output_order_metadata_signed_little_endian_and_no_allocation(self): self.run_case('output')
    def test_reset_preserves_inflight_and_stale_lease_cannot_release_reuse(self): self.run_case('reset')
    def test_serial_exhaustion_never_wraps_or_reopens_on_reset(self): self.run_case('exhaustion')


if __name__ == '__main__': unittest.main()

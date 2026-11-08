"""Portable CHICK-01 production model and renderer; run on cloud validation host."""
import os
import json
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]


class ChickFaceTests(unittest.TestCase):
    def test_production_model_and_bounded_renderer(self):
        display = FIRMWARE / 'overlays/main/display'
        for name in ('chick_face_model.h', 'chick_face_renderer.h'):
            self.assertTrue((display / name).is_file(), f'missing CHICK-01 production {name}')
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / 'chick_face'
            result = subprocess.run([
                os.environ.get('CXX', 'clang++'), '-std=c++17', '-O1', '-g',
                '-Wall', '-Wextra', '-Werror', '-pedantic',
                '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                '-I', str(display), str(FIRMWARE / 'tests/chick_face_host.cc'),
                '-o', str(binary)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                               UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            # Compare every scalar to the actual adopted source, independently
            # of the C++ preset construction code (including omitted defaults).
            source = (FIRMWARE.parent / 'docs/reference/chick-01/2026-11-08/'
                      'CHICK-01_屏幕表情系统清单_v1.0.html').read_text()
            source_rows = []
            for layer in ('L0', 'L1'):
                array = re.search(r'const ' + layer + r' = (\[.*?\n\]);', source, re.S)[1]
                array = re.sub(r'([,{]\s*)(\w+)\s*:', r'\1"\2":', array).replace("'", '"')
                source_rows.extend(json.loads(array))
            self.assertEqual(len(source_rows), 27)
            dump = subprocess.run([str(binary), '--presets'], capture_output=True, text=True,
                                  env=environment, timeout=20)
            self.assertEqual(dump.returncode, 0, dump.stderr)
            rendered_rows = json.loads(dump.stdout)
            self.assertEqual(len(rendered_rows), 27)
            for original, actual in zip(source_rows, rendered_rows):
                el = original['el']
                def eye(value):
                    return dict(shape=value.get('shape', 'open'), open=value.get('open', 1),
                                pupil_x=value.get('pupil', [0, 0])[0],
                                pupil_y=value.get('pupil', [0, 0])[1])
                mouth = el.get('mouth', {})
                brow = el.get('brow', {})
                expected = dict(left=eye(el.get('eyeL', el.get('eye', {}))),
                                right=eye(el.get('eyeR', el.get('eye', {}))),
                                mouth=dict(shape=mouth.get('shape', 'smile'),
                                           width=mouth.get('w', 1), open=mouth.get('open', 0)),
                                brow=dict(visible='brow' in el, y=brow.get('y', 0),
                                          angle=brow.get('angle', 0), split=brow.get('split', False)),
                                blush=el.get('blush', 0), effects=el.get('fx', []),
                                background=int(el.get('bg', '#14181F')[1:], 16), eye_color=0xFFD8A8)
                self.assertEqual(actual, expected, original['k'])
            result = subprocess.run([str(binary)], capture_output=True, text=True,
                                    env=environment, timeout=60)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('CHICK-01 model/render checks passed', result.stdout)
            print(result.stdout.strip())


if __name__ == '__main__':
    unittest.main()

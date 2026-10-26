"""Portable production orb state/rendering and actual DMA-envelope regressions."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]


def production_method(text, signature):
    start = text.index(signature)
    brace = text.index('{', start)
    depth = 0
    for end in range(brace, len(text)):
        depth += (text[end] == '{') - (text[end] == '}')
        if depth == 0:
            return text[start:end + 1]
    raise ValueError('unterminated production method')


class OrbUiTests(unittest.TestCase):
    def test_production_orb_state_render_and_completion(self):
        audio = FIRMWARE / 'overlays/main/audio'
        board = FIRMWARE / 'overlays/main/boards/m5stack/stopwatch'
        self.assertTrue((audio / 'orb_activity.h').is_file(), 'missing production audio activity observer')
        self.assertTrue((board / 'orb_model.h').is_file(), 'missing production orb state model')
        self.assertTrue((board / 'orb_renderer.h').is_file(), 'missing production orb renderer')
        self.assertTrue((board / 'orb_refresh_policy.h').is_file(), 'missing adaptive orb refresh policy')
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / 'orb'
            result = subprocess.run([
                'c++', '-std=c++20', '-Wall', '-Wextra', '-Werror', '-pedantic',
                '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                '-DCONFIG_XIAOMAN_ORB_UI=1', '-I', str(audio), '-I', str(board),
                str(FIRMWARE / 'tests/orb_ui_test.cc'), '-o', str(binary)],
                capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                               UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            result = subprocess.run([str(binary)], capture_output=True, text=True,
                                    env=environment, timeout=20)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            print(result.stdout.strip())

    def test_production_output_and_validated_protocol_observers(self):
        with tempfile.TemporaryDirectory() as directory:
            # Empty platform include files: this harness declares only the
            # LVGL types/functions used by real Update, not a replacement view.
            for header in ['sdkconfig.h', 'lvgl.h']:
                (Path(directory) / header).write_text('')
            for source in ['playback_adapter_test.cc', 'playback_websocket_test.cc',
                           'orb_display_host.cc', 'orb_refresh_view_host.cc']:
                binary = Path(directory) / source.removesuffix('.cc')
                program = FIRMWARE / 'tests' / source
                if source == 'orb_refresh_view_host.cc':
                    view = (FIRMWARE / 'overlays/main/boards/m5stack/stopwatch/xiaoman_orb_view.cc').read_text()
                    methods = [production_method(view, signature) for signature in [
                        'bool XiaomanOrbConsole(', 'bool XiaomanOrbView::Update(']]
                    program = Path(directory) / source
                    program.write_text((FIRMWARE / 'tests' / source).read_text().replace(
                        '// PRODUCTION_METHODS', '\n'.join(methods)))
                # LVGL's separate part/state enums require explicit selector
                # conversion in the target's C++23 build; exercise that warning.
                standard = 'c++20' if source == 'orb_display_host.cc' else 'c++17'
                enum_warning = ['-Werror=deprecated-enum-enum-conversion'] if source == 'orb_display_host.cc' else []
                result = subprocess.run([
                    'c++', f'-std={standard}', '-pthread', '-Wall', '-Wextra', '-Werror', '-pedantic',
                    *enum_warning,
                    '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                    '-DCONFIG_XIAOMAN_ORB_UI=1', '-I', directory,
                    '-I', str(FIRMWARE / 'overlays/main/audio'),
                    '-I', str(FIRMWARE / 'overlays/main/boards/m5stack/stopwatch'),
                    str(program), '-o', str(binary)], capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                environment = dict(os.environ)
                if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                result = subprocess.run([str(binary)], capture_output=True, text=True,
                                        env=environment, timeout=20)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                if source == 'orb_refresh_view_host.cc':
                    self.assertIn('orb_stats mode=0 frames=', result.stdout)
                    self.assertIn('psram=32768 requested=2', result.stdout)

    def test_production_layout_reserves_real_font_bounds(self):
        with tempfile.TemporaryDirectory() as directory:
            for header in ['sdkconfig.h', 'lvgl.h']:
                (Path(directory) / header).write_text('')
            view = (FIRMWARE / 'overlays/main/boards/m5stack/stopwatch/xiaoman_orb_view.cc').read_text()
            methods = [production_method(view, signature) for signature in [
                'XiaomanOrbView::~XiaomanOrbView(', 'void XiaomanOrbView::Initialize(',
                'void XiaomanOrbView::SetFont(', 'bool XiaomanOrbView::Update(']]
            program = Path(directory) / 'layout.cc'
            program.write_text((FIRMWARE / 'tests/orb_layout_host.cc').read_text().replace(
                '// PRODUCTION_METHODS', '\n'.join(methods)))
            binary = Path(directory) / 'layout'
            result = subprocess.run([
                'c++', '-std=c++20', '-Wall', '-Wextra', '-Werror', '-pedantic',
                '-Werror=deprecated-enum-enum-conversion',
                '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                '-DCONFIG_XIAOMAN_ORB_UI=1', '-I', directory,
                '-I', str(FIRMWARE / 'overlays/main/audio'),
                '-I', str(FIRMWARE / 'overlays/main/boards/m5stack/stopwatch'),
                str(program), '-o', str(binary)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            environment = dict(os.environ)
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            result = subprocess.run([str(binary)], capture_output=True, text=True,
                                    env=environment, timeout=20)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()

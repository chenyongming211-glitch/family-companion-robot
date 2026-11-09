"""Cloud host checks for the actual device bitmap and screen state model."""
from pathlib import Path
import os
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ScreenChecks(unittest.TestCase):
    def test_embedded_font_covers_every_offline_status(self):
        import re
        path = ROOT / 'overlays/onboarding/family_setup_font.c'
        self.assertTrue(path.is_file(), 'missing offline Chinese font')
        glyphs = {int(n) for n in re.findall(r'^\{(\d+),', path.read_text(), re.MULTILINE)}
        source = (ROOT / 'overlays/onboarding/family_onboarding.cc').read_text()
        for label in re.findall(r'(?:title|detail)="([^"]*)"', source):
            self.assertTrue(set(map(ord,label)) <= glyphs, label)

    def test_render_never_exposes_unvalidated_bitmap(self):
        from test_onboarding_lifecycle import function
        source = (ROOT / 'overlays/onboarding/family_onboarding.cc').read_text()
        program = r'''
#include "family_setup_state.h"
#include <vector>
#include <cassert>
using namespace FamilyOnboarding;
std::vector<uint8_t> screen_code(CodeSize,0);
bool screen_code_valid=false;
SetupScreenState screen_state;
const uint8_t* captured=nullptr;size_t captured_size=0;
struct Display {void SetOnboardingStatus(const char*,const char*,const uint8_t* bytes,size_t size){captured=bytes;captured_size=size;}};
struct Board {static Board& GetInstance(){static Board b;return b;}Display* GetDisplay(){static Display d;return &d;}};
'''+function(source,'void RenderSetup() {')+r'''
int main(){
 screen_state.Begin();RenderSetup();assert(captured==nullptr&&captured_size==0);
 screen_code_valid=true;RenderSetup();assert(captured==screen_code.data()+48&&captured_size==CodePixels);
 screen_state.phase=SetupPhase::CodeError;RenderSetup();assert(captured==nullptr&&captured_size==0);
}
'''
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory); (directory / 'test.cc').write_text(program)
            result = subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror',
                '-I',str(ROOT / 'overlays/onboarding'),str(directory / 'test.cc'),
                '-o',str(directory / 'test')],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            result = subprocess.run([str(directory / 'test')],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)

    def test_actual_view_geometry_and_owned_bitmap(self):
        source = ROOT / 'overlays/onboarding/family_setup_view.cc'
        self.assertTrue(source.is_file(), 'missing persistent device setup view')
        import re
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            (directory / 'lvgl.h').write_text('')
            production = re.sub(r'^#include .*\n', '', source.read_text(), flags=re.MULTILINE)
            host = (ROOT / 'tests/onboarding_view_host.cc').read_text()
            (directory / 'test.cc').write_text(host.replace('// PRODUCTION_VIEW', production))
            result = subprocess.run([
                os.environ.get('CXX', 'c++'), '-std=c++17', '-Wall', '-Wextra', '-Werror',
                '-I', str(directory), '-I', str(source.parent), str(directory / 'test.cc'),
                '-o', str(directory / 'test')], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            result = subprocess.run([str(directory / 'test')], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_bitmap_identity_crc_and_terminal_state(self):
        header = ROOT / 'overlays/onboarding/family_setup_state.h'
        self.assertTrue(header.is_file(), 'missing device screen validation/state implementation')
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / 'screen'
            result = subprocess.run([
                os.environ.get('CXX', 'c++'), '-std=c++17', '-Wall', '-Wextra', '-Werror',
                '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                '-I', str(header.parent), str(ROOT / 'tests/onboarding_screen_host.cc'),
                '-o', str(binary)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                               UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            result = subprocess.run([str(binary)], capture_output=True, text=True, env=environment)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()

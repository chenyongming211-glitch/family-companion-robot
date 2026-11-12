"""Compile actual bounded DMA state with music and voice clock boundaries."""
from pathlib import Path
import os
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]

class MusicPlaybackTests(unittest.TestCase):
    def test_dma_clock_and_receipt_boundaries(self):
        with tempfile.TemporaryDirectory() as tmp:
            binary=Path(tmp)/'music'
            result=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror',
                '-fsanitize=address,undefined','-fno-sanitize-recover=all',
                '-I',str(ROOT/'overlays/main/audio'),str(ROOT/'tests/music_playback_host.cc'),
                '-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            env=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                     UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            run=subprocess.run([str(binary)],env=env,capture_output=True,text=True,timeout=15)
            self.assertEqual(run.returncode,0,run.stdout+run.stderr)

    def test_actual_adapter_music_transition_and_restore(self):
        with tempfile.TemporaryDirectory() as tmp:
            work=Path(tmp);(work/'codecs').mkdir()
            (work/'codecs/box_audio_codec.h').write_text('// Platform supplied by harness.\n')
            (work/'sdkconfig.h').write_text('#define CONFIG_WAVESHARE_XIAOMAN_REALTIME 1\n')
            binary=work/'music-codec'
            board=ROOT/'overlays/main/boards/waveshare/esp32-s3-touch-amoled-2.16'
            result=subprocess.run(['c++','-std=c++17','-pthread','-Wall','-Wextra','-Werror',
                '-fsanitize=address,undefined','-fno-sanitize-recover=all',
                '-DCONFIG_CHICK_FACE_UI=1','-DCONFIG_FAMILY_MUSIC_AUDITION=1',
                '-I',str(work),'-I',str(board),'-I',str(ROOT/'overlays/main/audio'),
                str(ROOT/'tests/music_codec_host.cc'),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            env=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                     UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            run=subprocess.run([str(binary)],env=env,capture_output=True,text=True,timeout=20)
            self.assertEqual(run.returncode,0,run.stdout+run.stderr)

if __name__=='__main__':unittest.main()

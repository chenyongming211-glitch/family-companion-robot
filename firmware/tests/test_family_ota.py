"""Execute the production factory OTA parser, downloader and hardware guard."""
from pathlib import Path
import os, subprocess, tempfile, unittest
FIRMWARE=Path(__file__).resolve().parents[1]
def http_sdk_source():
    import hashlib,json,urllib.request
    path=Path(os.environ['IDF_PATH'])/'components/esp_http_client/esp_http_client.c' if os.environ.get('IDF_PATH') else FIRMWARE/'.cache/control-esp_http_client.c'
    digest='b28179259684e4cadbcaff00127ac50ea5c4477536cca1b4df86590694acdf24'
    if not path.is_file():
        lock=json.loads((FIRMWARE/'upstream.lock.json').read_text())
        with urllib.request.urlopen('https://raw.githubusercontent.com/espressif/esp-idf/'+lock['idf_commit']+'/components/esp_http_client/esp_http_client.c',timeout=30) as response:raw=response.read(512*1024+1)
        if hashlib.sha256(raw).hexdigest()!=digest:raise AssertionError('locked HTTP SDK mismatch')
        path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(raw)
    raw=path.read_bytes()
    if hashlib.sha256(raw).hexdigest()!=digest:raise AssertionError('locked HTTP SDK mismatch')
    return raw.decode()

class FamilyOtaTests(unittest.TestCase):
    def test_actual_protocol_and_streamed_sdk_transaction(self):
        cjson=Path(os.environ.get('FAMILY_CONTROL_CJSON_DIR',str(FIRMWARE/'.cache/control-cjson')))
        with tempfile.TemporaryDirectory() as folder:
            work=Path(folder); obj=work/'cjson.o'; binary=work/'ota'
            subprocess.run(['cc','-std=c11','-I'+str(cjson),'-c',str(cjson/'cJSON.c'),'-o',str(obj)],check=True)
            from test_onboarding_lifecycle import function
            sdk=http_sdk_source()
            source=work/'ota.cc';source.write_text((FIRMWARE/'tests/family_ota_host.cc').read_text().replace('// PRODUCTION_HTTP_OPEN',function(sdk,'esp_err_t esp_http_client_open(')))
            built=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror','-fsanitize=address,undefined','-fno-sanitize-recover=all','-I'+str(cjson),'-I'+str(FIRMWARE/'overlays/onboarding'),str(source),str(obj),'-lcrypto','-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(built.returncode,0,built.stderr)
            env=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            result=subprocess.run([str(binary)],capture_output=True,text=True,env=env,timeout=20)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)

class FamilyOtaCodecTests(unittest.TestCase):
    def test_actual_codec_checked_pause_and_fresh_input_restore(self):
        import test_realtime_default as defaults
        with tempfile.TemporaryDirectory() as folder:
            work=Path(folder);(work/'codecs').mkdir();(work/'codecs/box_audio_codec.h').write_text('// harness\n');(work/'sdkconfig.h').write_text('#define CONFIG_WAVESHARE_XIAOMAN_REALTIME 1\n#define CONFIG_FAMILY_MUSIC_AUDITION 1\n')
            binary=work/'ota-codec';board=FIRMWARE/'overlays/main/boards/waveshare/esp32-s3-touch-amoled-2.16'
            built=subprocess.run(['c++','-std=c++17','-pthread','-Wall','-Wextra','-Werror','-fsanitize=address,undefined','-fno-sanitize-recover=all','-DCONFIG_CHICK_FACE_UI=1','-DCONFIG_FAMILY_MUSIC_AUDITION=1','-I'+str(work),'-I'+str(FIRMWARE/'tests'),'-I'+str(board),'-I'+str(FIRMWARE/'overlays/main/audio'),str(FIRMWARE/'tests/family_ota_codec_host.cc'),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(built.returncode,0,built.stderr)
            env=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            result=subprocess.run([str(binary)],capture_output=True,text=True,env=env,timeout=20)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)

class FamilyOtaRuntimeTests(unittest.TestCase):
    def test_actual_single_task_boot_health_and_failure_restoration(self):
        cjson=Path(os.environ.get('FAMILY_CONTROL_CJSON_DIR',str(FIRMWARE/'.cache/control-cjson')))
        with tempfile.TemporaryDirectory() as folder:
            work=Path(folder);obj=work/'cjson.o';binary=work/'ota-runtime'
            subprocess.run(['cc','-std=c11','-I'+str(cjson),'-c',str(cjson/'cJSON.c'),'-o',str(obj)],check=True)
            built=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror','-I'+str(cjson),'-I'+str(FIRMWARE/'overlays/onboarding'),str(FIRMWARE/'tests/family_ota_runtime_host.cc'),str(obj),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(built.returncode,0,built.stderr)
            for case in ['pending-ok','pending-timeout','pending-marker-error','pending-marker-error-restore-error','pending-marker-error-restore-timeout','pending-restore-error','valid-fault','busy','stale','expiry-during-pause','task-error','download-error','retry','success']:
                result=subprocess.run([str(binary),case],capture_output=True,text=True,timeout=10)
                with self.subTest(case=case):self.assertEqual(result.returncode,0,case+': '+result.stdout+result.stderr)

class FamilyOtaApplicationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        import test_realtime_default as defaults
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)
    def test_actual_main_owner_hooks_and_reservation_state_guard(self):
        from test_onboarding_lifecycle import function
        with tempfile.TemporaryDirectory() as folder:
            import importlib.util,shutil
            work=Path(folder);source=work/'generated';(source/'main').mkdir(parents=True)
            for name in ['main/application.cc','main/application.h','main/audio/audio_service.h','CMakeLists.txt']:
                (source/name).parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(self.source/name,source/name)
            spec=importlib.util.spec_from_file_location('ota_prepare',FIRMWARE/'tools/prepare_ota.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
            # The seam expects onboarding's include, which stock deliberately lacks.
            app=source/'main/application.cc';app.write_text(app.read_text().replace('#include "application.h"','#include "application.h"\n#include "family_onboarding.h"'))
            module.prepare_ota(source)
            path=work/'application.cc';binary=work/'application'
            app=(source/'main/application.cc').read_text()
            path.write_text((FIRMWARE/'tests/family_ota_application_host.cc').read_text().replace('// PRODUCTION_SET_STATE',function(app,'bool Application::SetDeviceState(')))
            built=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror','-I'+str(FIRMWARE/'overlays/onboarding'),str(path),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(built.returncode,0,built.stderr)
            result=subprocess.run([str(binary)],capture_output=True,text=True,timeout=10)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)
            upgrade=function(app,'bool Application::UpgradeFirmware(')
            self.assertLess(upgrade.index('if(FamilyOnboarding::Present())return false;'),upgrade.index('audio_service_.Stop()'))
            self.assertIn('if(!FamilyOnboarding::Present())ota_->MarkCurrentVersionValid();',app)
            self.assertIn('!FamilyOnboarding::Present() && ota_->HasNewVersion()',app)
            runtime=(FIRMWARE/'overlays/onboarding/family_ota_runtime.inc').read_text()
            self.assertIn('if(pending_boot)Application::GetInstance().PrepareFamilyFirmwareBootAudio();',runtime)
            assets=(self.source/'main/assets.cc').read_text();self.assertIn('Assets::LoadSrmodelsFromIndex(assets, root.get());',assets)
            init=function(app,'void Application::Initialize() {')
            self.assertLess(init.index('audio_service_.Start();'),init.index('FamilyOta::InitializeBoot();'))
            self.assertLess(init.index('FamilyOta::InitializeBoot();'),init.index('board.StartNetwork();'))
            self.assertEqual((source/'CMakeLists.txt').read_text().count('set(PROJECT_VER "2.5.3")'),1)

class FamilyOtaHttpSdkTests(unittest.TestCase):
    def test_actual_sdk_async_header_and_body_progress_survive_eagain(self):
        from test_onboarding_lifecycle import function
        body=http_sdk_source()
        with tempfile.TemporaryDirectory() as folder:
            work=Path(folder);path=work/'http.cc';binary=work/'http'
            methods='\n'.join(function(body,s) for s in ['int64_t esp_http_client_fetch_headers(', 'int esp_http_client_read('])
            path.write_text((FIRMWARE/'tests/family_ota_http_sdk_host.cc').read_text().replace('// PRODUCTION_SDK_IO',methods))
            built=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror','-Wno-unused-variable','-Wno-unused-function','-Wno-unused-but-set-variable',str(path),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(built.returncode,0,built.stderr)
            result=subprocess.run([str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)

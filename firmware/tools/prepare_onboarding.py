#!/usr/bin/env python3
"""Prepare an isolated ESP32-S3 onboarding candidate; never flash or provision identity.
First prepare_experiment.py --destination TREE, then this tool. IDF/extra component
sources must match onboarding.lock.json; outputs are candidate sources only.
"""
import argparse, hashlib, json, pathlib, shutil, subprocess
FIRMWARE=pathlib.Path(__file__).resolve().parents[1]
def replace(path, old, new):
    text=path.read_text()
    if text.count(old)!=1: raise ValueError('source anchor mismatch: '+path.name)
    path.write_text(text.replace(old,new))
def prepare_s3_ble_memory(tree):
    # Shared factory-only host policy; controller/DMA and task stacks remain internal.
    with (tree/'sdkconfig.defaults').open('a') as output:
        output.write('\n# Both supported S3 onboarding profiles have PSRAM.\nCONFIG_BT_ALLOCATION_FROM_SPIRAM_FIRST=y\n')
def prepare_voice_boot(tree):
    application=tree/'main/application.cc'
    replace(application,'#include "application.h"','#include "application.h"\n#include "family_onboarding.h"')
    replace(application,'    // Setup the audio service',
        '    if(!FamilyOnboarding::PrepareVoiceOnBoot()) {SystemInfo::PrintHeapStats();vTaskDelete(nullptr);return;}\n'
        '    // Setup the audio service')
def prepare_configure_replay(tree):
    system_info=tree/'main/system_info.cc'
    replace(system_info,'#include "system_info.h"','#include "system_info.h"\n#include "family_onboarding.h"')
    replace(system_info,'void SystemInfo::PrintHeapStats() {',
        'void SystemInfo::PrintHeapStats() {\n    FamilyOnboarding::ReportConfigureStartup();')
def prepare_m5_screen(tree):
    # Only called on the isolated onboarding tree; stock prepare_experiment stays unchanged.
    board=tree/'main/boards/m5stack/stopwatch/m5stack_stopwatch.cc'
    replace(board,'#include "display/lcd_display.h"',
        '#include "display/lcd_display.h"\n#include "family_setup_view.h"\n#include "family_setup_font.h"')
    replace(board,'class RoundLcdDisplay : public SpiLcdDisplay {',
        'class RoundLcdDisplay : public SpiLcdDisplay {\n'
        '    FamilyOnboarding::SetupView family_setup_;\n'
        'public:\n'
        '    void SetOnboardingStatus(const char* title,const char* detail,const uint8_t* packed,size_t size) override {\n'
        '#if CONFIG_CHICK_FACE_UI\n'
        '        SetFaceScreenOn(title==nullptr);\n'
        '#endif\n'
        '        DisplayLockGuard lock(this);if(!lock || !IsSetupUICalled())return;\n'
        '        family_setup_.Initialize(&family_setup_font,width_,height_);\n'
        '        family_setup_.Update(title,detail,packed,size);\n'
        '    }\n')
    partition=tree/'partitions/v2/16m.csv'
    replace(partition,'nvs,      data, nvs,     0x9000,    0x4000,','nvs,      data, nvs,     0x9000,    0x6000,')
    replace(partition,'otadata,  data, ota,     0xd000,    0x2000,','otadata,  data, ota,     0xf000,    0x2000,')
    replace(partition,'phy_init, data, phy,     0xf000,    0x1000,','phy_init, data, phy,     0x11000,   0x1000,')
def main():
    p=argparse.ArgumentParser();p.add_argument('--tree',type=pathlib.Path,required=True);p.add_argument('--idf-source',type=pathlib.Path,required=True);p.add_argument('--extra-source',type=pathlib.Path,required=True);args=p.parse_args()
    tree=args.tree.resolve(); idf=args.idf_source.resolve(); extra=args.extra_source.resolve()
    lock=json.loads((FIRMWARE/'onboarding.lock.json').read_text())
    provenance = json.loads((tree/'.family-robot-source.json').read_text())
    if provenance['upstream_commit'] != lock['upstream_commit'] or provenance.get('target_dependency_overrides'): raise ValueError('requires locked S3 upstream')
    if (tree/'sdkconfig').exists() or (tree/'.family-onboarding-source.json').exists(): raise ValueError('requires fresh unconfigured candidate')
    # Every SDK/component source is verified against manifest generated from pinned official trees.
    for name,source in [('idf',idf),('extra',extra)]:
        for relative,digest in lock[name+'_files'].items():
            if hashlib.sha256((source/relative).read_bytes()).hexdigest()!=digest: raise ValueError('official source mismatch')
    components=tree/'components'; components.mkdir(exist_ok=True)
    shutil.copytree(idf/'components/protocomm',components/'protocomm')
    shutil.copytree(extra/'network_provisioning',components/'network_provisioning')
    shutil.copytree(extra/'json_generator',components/'json_generator')
    for path in sorted((FIRMWARE/'patches/onboarding').glob('*.patch')):
        subprocess.run(['git','apply','--check',str(path)],cwd=tree,check=True)
        subprocess.run(['git','apply',str(path)],cwd=tree,check=True)
    for name in ['protocomm_ble_framing.h','protocomm_ble_framed_transport.inc']:
        shutil.copyfile(FIRMWARE/'overlays/onboarding'/name,components/'protocomm/src/transports'/name)
    for name in ['family_onboarding.h','family_onboarding.cc','family_setup_state.h','family_setup_view.h','family_setup_view.cc','family_setup_font.h','family_setup_font.c']:
        shutil.copyfile(FIRMWARE/'overlays/onboarding'/name,tree/'main'/name)
    replace(tree/'main/CMakeLists.txt','            "ota.cc"','            "ota.cc"\n            "family_onboarding.cc"\n            "family_setup_view.cc"\n            "family_setup_font.c"')
    replace(tree/'main/CMakeLists.txt','                    PRIV_REQUIRES','                    PRIV_REQUIRES\n                        network_provisioning\n                        esp_http_client\n                        esp_wifi\n                        bt')
    replace(tree/'main/idf_component.yml', 'dependencies:\n', 'dependencies:\n  espressif/esp_jpeg: ==1.3.1\n  espressif/iot_usbh_cdc: ==3.1.0\n')
    replace(tree/'main/display/display.h','    virtual void SetStatus(const char* status);',
        '    virtual void SetStatus(const char* status);\n'
        '    virtual void SetOnboardingStatus(const char*,const char*,const uint8_t*,size_t) {}')
    screen_board=tree/'main/boards/waveshare/esp32-s3-touch-amoled-2.16/esp32-s3-touch-amoled-2.16.cc'
    replace(screen_board,'#include "display/lcd_display.h"',
        '#include "display/lcd_display.h"\n#include "family_setup_view.h"\n#include "family_setup_font.h"\n#include "display/lvgl_display/lvgl_font.h"')
    replace(screen_board,'class CustomLcdDisplay : public SpiLcdDisplay {',
        'class CustomLcdDisplay : public SpiLcdDisplay {\n'
        '    FamilyOnboarding::SetupView family_setup_;\n'
        'public:\n'
        '    void SetOnboardingStatus(const char* title,const char* detail,const uint8_t* packed,size_t size) override {\n'
        '#if CONFIG_CHICK_FACE_UI\n'
        '        SetFaceScreenOn(title==nullptr);\n'
        '#endif\n'
        '        DisplayLockGuard lock(this);if(!lock || !IsSetupUICalled())return;\n'
        '        family_setup_.Initialize(&family_setup_font,width_,height_);\n'
        '        family_setup_.Update(title,detail,packed,size);\n'
        '    }\n')
    prepare_m5_screen(tree)
    prepare_s3_ble_memory(tree)
    prepare_configure_replay(tree)
    prepare_voice_boot(tree)
    board=tree/'main/boards/common/wifi_board.cc'
    replace(board,'#include "wifi_board.h"','#include "wifi_board.h"\n#include "family_onboarding.h"')
    replace(board,'void WifiBoard::StartNetwork() {','void WifiBoard::StartNetwork() {\n    if (FamilyOnboarding::Present()) {\n        FamilyOnboarding::Start([this](bool connected) {\n            in_config_mode_ = !FamilyOnboarding::Activated();\n            if (network_event_callback_) network_event_callback_(connected ? NetworkEvent::Connected : NetworkEvent::Disconnected, "");\n        });\n        return;\n    }')
    replace(board,'void WifiBoard::StartWifiConfigMode() {','void WifiBoard::StartWifiConfigMode() {\n    if (FamilyOnboarding::Present()) { FamilyOnboarding::Configure(); return; }')
    replace(board,'void WifiBoard::EnterWifiConfigMode() {','void WifiBoard::EnterWifiConfigMode() {\n    if (FamilyOnboarding::Present()) { FamilyOnboarding::Configure(); return; }')
    replace(board,'const char* WifiBoard::GetNetworkStateIcon() {','const char* WifiBoard::GetNetworkStateIcon() {\n    if (FamilyOnboarding::Present()) return MATERIAL_SYMBOLS_WIFI;')
    replace(board,'std::string WifiBoard::GetBoardJson() {','std::string WifiBoard::GetBoardJson() {\n    if (FamilyOnboarding::Present()) return "{\\"type\\":\\"wifi\\",\\"name\\":\\"family-s3\\"}";')
    replace(board,'void WifiBoard::SetPowerSaveLevel(PowerSaveLevel level) {','void WifiBoard::SetPowerSaveLevel(PowerSaveLevel level) {\n    if (FamilyOnboarding::Present()) return;')
    replace(board,'bool WifiBoard::IsInWifiConfigMode() const {','bool WifiBoard::IsInWifiConfigMode() const {\n    if (FamilyOnboarding::Present()) return !FamilyOnboarding::Activated();')
    ota=tree/'main/ota.cc'
    replace(ota,'#include "ota.h"','#include "ota.h"\n#include "family_onboarding.h"')
    replace(ota,'std::string Ota::GetCheckVersionUrl() {','std::string Ota::GetCheckVersionUrl() {\n    if (FamilyOnboarding::Present()) return FamilyOnboarding::Bootstrap;')
    replace(ota,'    auto http = network->CreateHttp(0);\n    auto user_agent', '    auto http = network->CreateHttp(0);\n    if (FamilyOnboarding::Present()) http->SetHeader("Authorization", "Bearer " + FamilyOnboarding::Key());\n    auto user_agent')
    ws=tree/'main/protocols/websocket_protocol.cc'
    replace(ws,'#include "websocket_protocol.h"','#include "websocket_protocol.h"\n#include "family_onboarding.h"')
    replace(ws,'    int version = settings.GetInt("version");','    int version = settings.GetInt("version");\n    if (FamilyOnboarding::Present()) {\n        if (!FamilyOnboarding::Activated()) return false;\n        url = FamilyOnboarding::Voice; token = FamilyOnboarding::Key(); version = 1;\n        if (token.size() != 43) return false;\n    }')
    # New factory units only: use the previously unused tail for two larger OTA slots.
    partition = tree/'partitions/waveshare-xiaoman-16m.csv'
    replace(partition, '# Name,Type,SubType,Offset,Size', '# NEW FACTORY FULL FLASH ONLY; incompatible with legacy app-only OTA\n# Name,Type,SubType,Offset,Size')
    replace(partition, 'ota_0,app,ota_0,0x20000,0x300000', 'ota_0,app,ota_0,0x20000,0x370000')
    replace(partition, 'ota_1,app,ota_1,0x320000,0x300000', 'ota_1,app,ota_1,0x390000,0x370000')
    replace(partition, 'assets,data,spiffs,0x620000,0x600000', 'assets,data,spiffs,0x700000,0x600000')
    replace(partition, 'storage,data,spiffs,0xc20000,0x300000', 'storage,data,spiffs,0xd00000,0x300000')
    with (tree/'sdkconfig.defaults').open('a') as f:
        f.write('\n# Factory-only Security2; legacy factory-absent branch remains upstream.\nCONFIG_OTA_URL="https://robot.songguoxue.com/api/device/bootstrap"\nCONFIG_BT_ENABLED=y\nCONFIG_BT_BLUEDROID_ENABLED=y\nCONFIG_BT_BLE_ENABLED=y\nCONFIG_BT_BLE_42_FEATURES_SUPPORTED=y\nCONFIG_BT_BLE_50_FEATURES_SUPPORTED=n\nCONFIG_ESP_PROTOCOMM_SUPPORT_SECURITY_VERSION_2=y\nCONFIG_MBEDTLS_CERTIFICATE_BUNDLE=y\nCONFIG_MBEDTLS_HAVE_TIME_DATE=y\nCONFIG_NETWORK_PROV_NETWORK_TYPE_WIFI=y\n')
    dependencies = FIRMWARE/'dependencies.onboarding.lock'
    if hashlib.sha256(dependencies.read_bytes()).hexdigest() != dependencies.with_name(dependencies.name+'.sha256').read_text().strip(): raise ValueError('onboarding dependency lock mismatch')
    shutil.copyfile(dependencies, tree/'dependencies.lock')
    (tree/'.family-onboarding-source.json').write_text(json.dumps({'lock_sha256':hashlib.sha256((FIRMWARE/'onboarding.lock.json').read_bytes()).hexdigest(),'target':'esp32s3','new_factory_full_flash_only': True,'physical_validation':'not_run', 'inputs': {str(p.relative_to(FIRMWARE)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted([*list((FIRMWARE/'overlays/onboarding').glob('*')), *list((FIRMWARE/'patches/onboarding').glob('*.patch')), pathlib.Path(__file__).resolve(), dependencies]) if p.is_file()}},indent=2)+'\n')
if __name__=='__main__': main()

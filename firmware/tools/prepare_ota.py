"""Small authenticated factory OTA seams; stock URL behavior remains upstream."""
def replace(path,old,new):
    text=path.read_text()
    if text.count(old)!=1:raise ValueError('factory OTA source anchor mismatch: '+path.name+' '+old[:70])
    path.write_text(text.replace(old,new))
def prepare_ota(tree):
    app=tree/'main/application.cc'
    replace(app,'#include "family_onboarding.h"','#include "family_onboarding.h"\n#include "family_ota.h"\n#include "family_control_state.h"')
    replace(app,'bool Application::SetDeviceState(DeviceState state) {','bool Application::SetDeviceState(DeviceState state) {\n    if(FamilyOta::Reserved() && state!=kDeviceStateUpgrading)return false;')
    replace(app,'    // Start network asynchronously','    FamilyOta::InitializeBoot();\n    // Start network asynchronously')
    replace(app,'            clock_ticks_++;','            clock_ticks_++;\n            FamilyOta::Tick();')
    replace(app,'        ota_->MarkCurrentVersionValid();','        if(!FamilyOnboarding::Present())ota_->MarkCurrentVersionValid();')
    replace(app,'        if (ota_->HasNewVersion()) {','        if (!FamilyOnboarding::Present() && ota_->HasNewVersion()) {')
    replace(app,'bool Application::UpgradeFirmware(const std::string& url, const std::string& version) {','bool Application::UpgradeFirmware(const std::string& url, const std::string& version) {\n    if(FamilyOnboarding::Present())return false; // authenticated management job only, before stopping audio')
    replace(app,'void Application::Reboot() {','void Application::Reboot() {\n    if(FamilyOta::Reserved())return;')
    replace(app,'void Application::StartNotification(std::string audio_url, std::vector<NotifySubtitle> subtitles) {','void Application::StartNotification(std::string audio_url, std::vector<NotifySubtitle> subtitles) {\n    if(FamilyOta::Reserved())return;')
    # Alerts may come from old protocol callbacks after the OTA reservation.
    replace(app,'void Application::Alert(const char* status, const char* message, const char* emotion,\n                        const std::string_view& sound) {','void Application::Alert(const char* status, const char* message, const char* emotion,\n                        const std::string_view& sound) {\n    if(FamilyOta::Reserved())return;')
    with app.open('a') as f:f.write('\n#include "family_ota_application.inc"\n')
    header=tree/'main/application.h'
    replace(header,'    bool CanEnterSleepMode();','    bool CanEnterSleepMode();\n    bool PrepareFamilyFirmwareBootAudio();\n    bool FamilyFirmwareEligible();\n    bool BeginFamilyFirmware();\n    bool FamilyFirmwareQuiescent();\n    void PauseFamilyFirmwareInput();\n    void ResumeFamilyFirmwareInput();\n    void FinishFamilyFirmware();')
    replace(tree/'main/audio/audio_service.h','    bool IsIdle();','    bool IsIdle();\n    bool IsAudioTestingRunning() const {return xEventGroupGetBits(event_group_) & AS_EVENT_AUDIO_TESTING_RUNNING;}')
    replace(tree/'CMakeLists.txt','set(PROJECT_VER "2.5.2")','set(PROJECT_VER "2.5.4")')

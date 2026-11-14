namespace FamilyOta {
namespace {
std::atomic<bool> reserved{false},queued{false},ready{false};
std::mutex report_mutex;
std::string job_id,last_job_id;
uint64_t report_revision=1;
const char* phase="checking";const char* error="";int progress=0;
bool enabled=false,pending_boot=false,healthy_boot=false,restoring=false,waiting=false,marker_waiting=false,marker_failed=false,health_failed=false;
uint64_t boot_deadline=0,restore_deadline=0,start_deadline=0,restart_at=0;
const char* restore_error="";
Command active_command;
void Status(const char* value,const char* failure="") {
    std::lock_guard<std::mutex> guard(report_mutex);phase=value;error=failure;++report_revision;
}
void Progress(int value) {std::lock_guard<std::mutex> guard(report_mutex);progress=value;}
void Restore(const char* failure) {
    auto& app=Application::GetInstance();auto codec=Board::GetInstance().GetAudioCodec();
    waiting=false;restore_error=failure;restore_deadline=Now()+3000;
    const bool restored=codec->ResumeAfterFirmware();
    if(restored){restoring=true;app.ResumeFamilyFirmwareInput();}
    else {restoring=false;health_failed=true;codec->ReportReferenceFailure();Status("failed","health_failed");ready=false;}
}
void UpgradeTask(void*) {
    const auto failure=Download(active_command);
    Application::GetInstance().Schedule([failure]{
        if(failure)Restore(failure);
        else {Status("restarting");restart_at=Now()+6000;}
    });
    vTaskDelete(nullptr);
}
void BootFailure() {
    Status("failed","health_failed");ready=false;pending_boot=false;
    auto codec=Board::GetInstance().GetAudioCodec();
    Application::GetInstance().PauseFamilyFirmwareInput();
    if(codec->PauseForFirmware())esp_ota_mark_app_invalid_rollback_and_reboot();
    // Never called after a successful mark-valid or for an already valid boot.
    esp_restart();
}
}
bool Reserved(){return reserved.load();}
bool Ready(){return ready.load();}
void InitializeBoot() {
    enabled=FamilyOnboarding::Present();if(!enabled)return;
    if(!CompatibleLayout()){enabled=false;Status("failed","image_mismatch");return;}
    esp_ota_img_states_t state{};
    pending_boot=esp_ota_get_state_partition(esp_ota_get_running_partition(),&state)==ESP_OK && state==ESP_OTA_IMG_PENDING_VERIFY;
    boot_deadline=Now()+30000;
    if(pending_boot)Application::GetInstance().PrepareFamilyFirmwareBootAudio();
}
void Tick() {
    if(!enabled)return;
    if(health_failed){if(pending_boot && Now()>=boot_deadline)BootFailure();return;}
    auto codec=Board::GetInstance().GetAudioCodec();auto& app=Application::GetInstance();
    if(restoring) {
        if(codec->FirmwareHealthy() && codec->FinishFirmwareResume()) {
            restoring=false;healthy_boot=!pending_boot;ready=healthy_boot && codec->FirmwareAvailable() && DownloadAvailable();
            if(reserved.exchange(false)){FamilyControl::ReleaseFirmware();app.FinishFamilyFirmware();}
            if(*restore_error)Status("failed",restore_error);else Status("busy");
        } else if(Now()>=restore_deadline || codec->ReferenceFaulted()) {
            restoring=false;health_failed=true;ready=false;codec->ReportReferenceFailure();Status("failed","health_failed");
        }
        return;
    }
    if(restart_at){if(Now()>=restart_at)esp_restart();return;}
    if(waiting) {
        if(Now()>=start_deadline){Restore("timeout");return;}
        if(!app.FamilyFirmwareQuiescent())return;
        waiting=false;
        if(!codec->PauseForFirmware()){Restore("health_failed");return;}
        if(Now()>=start_deadline){Restore("timeout");return;}
        Status("downloading");
        if(xTaskCreate(UpgradeTask,"family_ota",8192,nullptr,3,nullptr)!=pdPASS)Restore("download_failed");
        return;
    }
    if(reserved)return;
    if(pending_boot) {
        if(marker_failed){BootFailure();return;}
        if(marker_waiting || codec->FirmwareHealthy()) {
            // Every mark-valid write uses the same hardware pause as download.
            // marker success clears rollback protection before audio restoration.
            // A later restore failure stays muted/failed; it cannot claim pending.
            if(!marker_waiting){app.PauseFamilyFirmwareInput();marker_waiting=true;}
            if(!app.FamilyFirmwareQuiescent()){if(Now()>=boot_deadline)BootFailure();return;}
            if(!codec->PauseForFirmware()){if(Now()>=boot_deadline)BootFailure();return;}
            const auto marked=esp_ota_mark_app_valid_cancel_rollback();
            if(marked==ESP_OK)pending_boot=false;else marker_failed=true;
            marker_waiting=false;
            Restore(marked==ESP_OK?"":"boot_failed");
        } else if(Now()>=boot_deadline)BootFailure();
        return;
    }
    if(!healthy_boot && codec->FirmwareHealthy())healthy_boot=true;
    const bool available=healthy_boot && codec->FirmwareAvailable() && DownloadAvailable();
    ready=available;
    std::lock_guard<std::mutex> guard(report_mutex);
    if(std::strcmp(phase,"failed")!=0){const char* next=!healthy_boot?"checking":available&&app.FamilyFirmwareEligible()?"ready":"busy";if(phase!=next){phase=next;++report_revision;}}
}
std::string Report(const std::string& boot,uint64_t seq,uint64_t* revision) {
    if(!enabled||!seq||seq>9007199254740991ULL)return {};
    const auto descriptor=esp_app_get_description();
    auto root=cJSON_CreateObject();if(!root)return {};
    cJSON_AddNumberToObject(root,"capability",1);cJSON_AddStringToObject(root,"boot",boot.c_str());cJSON_AddNumberToObject(root,"seq",static_cast<double>(seq));
    cJSON_AddStringToObject(root,"version",descriptor->version);cJSON_AddStringToObject(root,"elfSha256",Hex(descriptor->app_elf_sha256,32).c_str());
    cJSON_AddStringToObject(root,"layout","waveshare-s3-16m-v1");cJSON_AddNumberToObject(root,"slotBytes",SlotBytes);
    {std::lock_guard<std::mutex> guard(report_mutex);if(revision)*revision=report_revision;cJSON_AddStringToObject(root,"phase",phase);cJSON_AddStringToObject(root,"jobId",job_id.c_str());cJSON_AddNumberToObject(root,"progress",progress);cJSON_AddStringToObject(root,"error",error);}
    char* encoded=cJSON_PrintUnformatted(root);cJSON_Delete(root);if(!encoded)return {};
    std::string result(encoded);cJSON_free(encoded);return result;
}
void Receive(const std::string& body,uint64_t received_at,uint64_t revision) {
    Command command;
    if(!ParseResponse(body,command)||!enabled)return;
    if(command.id.empty()) {
        if(!revision||reserved.load()||!DownloadAvailable())return;
        {std::lock_guard<std::mutex> guard(report_mutex);if(revision!=report_revision||job_id.empty()||std::strcmp(phase,"failed")!=0)return;}
        if(queued.exchange(true))return;
        Application::GetInstance().Schedule([revision]{
            queued=false;
            std::lock_guard<std::mutex> guard(report_mutex);
            if(!reserved.load()&&revision==report_revision&&std::strcmp(phase,"failed")==0){job_id.clear();error="";progress=0;phase="checking";++report_revision;}
        });
        return;
    }
    if(reserved.load())return;
    {std::lock_guard<std::mutex> guard(report_mutex);if(command.id==last_job_id)return;}
    if(queued.exchange(true))return;
    Application::GetInstance().Schedule([command,received_at]{
        queued=false;
        if(reserved.load())return;
        {std::lock_guard<std::mutex> guard(report_mutex);if(command.id==last_job_id)return;job_id=last_job_id=command.id;progress=0;++report_revision;}
        if(Now()-received_at>=15000){Status("failed","timeout");return;}
        if(!ready.load()||pending_boot||restoring||!FamilyControl::ReserveFirmware()){Status("failed","busy");return;}
        reserved=true;
        if(!Application::GetInstance().BeginFamilyFirmware()) {
            reserved=false;FamilyControl::ReleaseFirmware();Status("failed","busy");return;
        }
        active_command=command;start_deadline=received_at+15000;waiting=true;ready=false;Status("checking");
    });
}
}

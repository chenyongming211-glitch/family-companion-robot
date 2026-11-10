#include "family_control_state.h"
#include "family_control_protocol.h"
#include "family_onboarding.h"
#include "application.h"
#include "board.h"
#include "audio_codec.h"
#include "backlight.h"
#include "system_info.h"
#include <atomic>
#include <mutex>
#include <esp_crt_bundle.h>
#include <esp_http_client.h>
#include <esp_log.h>
#include <esp_random.h>
#include <esp_timer.h>
#include <freertos/FreeRTOS.h>
#include <freertos/semphr.h>
#include <freertos/task.h>

namespace FamilyControl {
namespace {
constexpr const char* Endpoint="https://robot.songguoxue.com/api/device/state";
std::atomic<bool> online{false},started{false};
std::mutex mutex;
State state;
SemaphoreHandle_t signal=nullptr;
uint64_t wanted=0,completed=0;
bool snapshot_queued=false;
int volume=-1,brightness=-1;
uint64_t Now(){return static_cast<uint64_t>(esp_timer_get_time()/1000);}
bool Snapshot() {
    while(xSemaphoreTake(signal,0)==pdTRUE){}
    uint64_t job;
    {std::lock_guard<std::mutex> guard(mutex);if(snapshot_queued)return false;snapshot_queued=true;job=++wanted;}
    Application::GetInstance().Schedule([job]{
        std::lock_guard<std::mutex> guard(mutex);
        snapshot_queued=false;
        if(job!=wanted||!online)return;
        auto& board=Board::GetInstance();auto codec=board.GetAudioCodec();auto light=board.GetBacklight();
        if(auto command=state.Take(Now())) {
            if(command->field==Field::Volume&&codec)codec->SetOutputVolume(command->value);
            if(command->field==Field::Brightness&&light)light->SetBrightness(command->value,true);
        }
        volume=codec?codec->output_volume():-1;brightness=light?light->brightness():-1;
        state.Observe(volume,brightness,Now());completed=job;xSemaphoreGive(signal);
    });
    const bool notified=xSemaphoreTake(signal,pdMS_TO_TICKS(1000))==pdTRUE;
    std::lock_guard<std::mutex> guard(mutex);
    if(!notified||completed!=job){++wanted;return false;}
    return true;
}
struct Response{std::string body;bool overflow=false;};
esp_err_t Event(esp_http_client_event_t* event) {
    auto response=static_cast<Response*>(event->user_data);
    if(event->event_id==HTTP_EVENT_ON_DATA) {
        if(event->data_len<0||response->body.size()+static_cast<size_t>(event->data_len)>2048){response->overflow=true;return ESP_FAIL;}
        response->body.append(static_cast<const char*>(event->data),event->data_len);
    }
    return ESP_OK;
}
void Worker(void*) {
    uint8_t random[16];esp_fill_random(random,sizeof(random));
    const char digits[]="0123456789abcdef";std::string boot;boot.reserve(32);
    for(auto byte:random){boot+=digits[byte>>4];boot+=digits[byte&15];}
    uint64_t seq=0;unsigned failures=0;Response response;
    esp_http_client_config_t config{};
    config.url=Endpoint;config.crt_bundle_attach=esp_crt_bundle_attach;
    config.timeout_ms=5000;config.disable_auto_redirect=true;config.buffer_size=2048;config.buffer_size_tx=2048;
    config.event_handler=Event;config.user_data=&response;
    auto http=esp_http_client_init(&config);
    if(!http){ESP_LOGE("FamilyControl","client initialization failed");started=false;vTaskDelete(nullptr);return;}
    const auto authorization="Bearer "+FamilyOnboarding::Key();
    esp_http_client_set_header(http,"Authorization",authorization.c_str());
    esp_http_client_set_header(http,"Device-Id",SystemInfo::GetMacAddress().c_str());
    esp_http_client_set_method(http,HTTP_METHOD_POST);
    while(true) {
        if(!online||!FamilyOnboarding::Activated()){esp_http_client_close(http);vTaskDelay(pdMS_TO_TICKS(1000));continue;}
        if(!Snapshot()){vTaskDelay(pdMS_TO_TICKS(1000));continue;}
        std::string body;
        {std::lock_guard<std::mutex> guard(mutex);body=Report(boot,++seq,volume,brightness,state);}
        bool valid=false;
        if(!body.empty()) {
            response.body.clear();response.overflow=false;
            // Clearing the previous POST pointer also deletes Content-Type.
            esp_http_client_set_header(http,"Content-Type","application/json");
            esp_http_client_set_post_field(http,body.c_str(),body.size());
            const auto result=esp_http_client_perform(http);
            const auto status=esp_http_client_get_status_code(http);
            // The client retains the POST pointer. Clear it before body dies.
            esp_http_client_set_post_field(http,nullptr,0);
            Command command;
            valid=result==ESP_OK&&status==200&&!response.overflow&&ParseResponse(response.body,command);
            if(valid&&!command.id.empty()&&online) {
                {std::lock_guard<std::mutex> guard(mutex);state.Offer(command,Now());}
                Snapshot();
            }
        }
        if(!valid){esp_http_client_close(http);failures=failures<3?failures+1:3;}else failures=0;
        const unsigned delay=failures==0?5:failures==1?10:failures==2?20:30;
        for(unsigned i=0;i<delay&&online;++i)vTaskDelay(pdMS_TO_TICKS(1000));
    }
}
}
void Connected(bool connected) {
    online=connected;
    if(!connected||!FamilyOnboarding::Activated()||started.exchange(true))return;
    if(!signal)signal=xSemaphoreCreateBinary();
    if(!signal||xTaskCreate(Worker,"family_control",8192,nullptr,3,nullptr)!=pdPASS) {
        started=false;ESP_LOGE("FamilyControl","worker creation failed");
    }
}
}

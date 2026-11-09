#include "family_onboarding.h"
#include "family_setup_state.h"
#include "settings.h"
#include "system_info.h"
#include <esp_crt_bundle.h>
#include <esp_event.h>
#include <esp_http_client.h>
#include <esp_netif.h>
#include <esp_netif_sntp.h>
#include <esp_log.h>
#include <esp_heap_caps.h>
#include <esp_bt.h>
#include <esp_bt_main.h>
#include "board.h"
#include "display.h"
#include <esp_timer.h>
#include <esp_wifi.h>
#include <nvs.h>
#include <cJSON.h>
#include <network_provisioning/manager.h>
#include <network_provisioning/scheme_ble.h>
#include <freertos/FreeRTOS.h>
#include <freertos/task.h>
#include <freertos/semphr.h>
#include <atomic>
#include <cstring>
#include <cctype>
#include <algorithm>
#include <vector>

namespace FamilyOnboarding {
namespace {
ESP_EVENT_DEFINE_BASE(FAMILY_CONNECTION_EVENT);
constexpr int ClockReady = 1;
constexpr int64_t WindowUs = 180LL * 1000000;
std::function<void(bool)> callback;
SemaphoreHandle_t lock;
std::string claim_id, challenge;
std::vector<uint8_t> salt, verifier;
std::vector<uint8_t> screen_code;
bool screen_code_valid=false;
SetupScreenState screen_state;
network_prov_security2_params_t security{};
std::atomic<bool> connected{false}, running{false}, worker_active{false}, clock_worker{false}, clock_synced{false}, connection_reported{false};
std::atomic<uint32_t> network_generation{0};
std::atomic<int64_t> deadline{0};
bool initialized = false;
bool manager_ready = false;
bool endpoint_created = false;
std::atomic<bool> voice_resources_ready{false};
esp_timer_handle_t expiry_timer = nullptr;

bool VoiceResourcesReady() {
    return voice_resources_ready.load();
}
struct ConfigureStartupSnapshot {
    const char* stage=nullptr;
    int result=0;
    unsigned internal=0,largest=0,free8=0;
    int reclaimed=0;unsigned resource_stage=0,prior_internal=0;
};
ConfigureStartupSnapshot configure_startup;
bool configure_startup_pending=false;
portMUX_TYPE configure_startup_mux=portMUX_INITIALIZER_UNLOCKED;

// Only fixed Configure stage literals and numeric allocator/API facts are logged.
void LogConfigure(const char* stage,int result,int reclaimed=0,unsigned resource_stage=0,unsigned prior_internal=0) {
    constexpr uint32_t caps=MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT;
    const ConfigureStartupSnapshot snapshot{stage,result,
        resource_stage?static_cast<unsigned>(static_cast<int>(prior_internal)+reclaimed):static_cast<unsigned>(heap_caps_get_free_size(caps)),
        static_cast<unsigned>(heap_caps_get_largest_free_block(caps)),
        static_cast<unsigned>(heap_caps_get_free_size(MALLOC_CAP_8BIT)),reclaimed,resource_stage,prior_internal};
    portENTER_CRITICAL(&configure_startup_mux);
    configure_startup=snapshot;configure_startup_pending=true;
    portEXIT_CRITICAL(&configure_startup_mux);
    ESP_LOGI("FamilySetup","configure stage=%s err=%d internal=%u largest=%u free8=%u reclaimed=%d resource_stage=%u prior_internal=%u",
        snapshot.stage,snapshot.result,snapshot.internal,snapshot.largest,snapshot.free8,snapshot.reclaimed,snapshot.resource_stage,snapshot.prior_internal);
}

bool PrepareActivatedVoice() {
    if(!Activated())return false; // Never make an unbound retry's BLE memory unavailable.
    if(VoiceResourcesReady())return true;
    const auto before=heap_caps_get_free_size(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    unsigned stage=1;
    auto result=ESP_OK;
    if(manager_ready) {
        result=network_prov_mgr_deinit(); // Blocking: no voice callback until transport teardown completes.
        if(result==ESP_OK) {manager_ready=false;endpoint_created=false;}
    }
    if(result==ESP_OK) {
        stage=2;
        if(esp_bt_controller_get_status()!=ESP_BT_CONTROLLER_STATUS_IDLE ||
            esp_bluedroid_get_status()!=ESP_BLUEDROID_STATUS_UNINITIALIZED)result=ESP_ERR_INVALID_STATE;
    }
    if(result==ESP_OK) {stage=3;result=esp_bt_mem_release(ESP_BT_MODE_BLE);}
    const auto after=heap_caps_get_free_size(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    LogConfigure("voice_bt_cleanup",result,static_cast<int>(after)-static_cast<int>(before),stage,static_cast<unsigned>(before));
    if(result==ESP_OK)voice_resources_ready=true;
    return result==ESP_OK;
}

void RenderSetup() {
    const char *title="", *detail="";
    bool code=false;
    switch(screen_state.phase) {
    case SetupPhase::Hidden:
        Board::GetInstance().GetDisplay()->SetOnboardingStatus(nullptr,nullptr,nullptr,0);return;
    case SetupPhase::Waiting:
        title="微信扫码添加设备";detail="扫码后按提示配网";code=true;break;
    case SetupPhase::Connecting:
        title="正在连接 Wi-Fi";detail="请保持设备通电，稍候片刻";break;
    case SetupPhase::WifiError:
        title="Wi-Fi 连接失败";detail="检查网络密码重试";
#if !CONFIG_BOARD_TYPE_M5STACK_STOPWATCH
        code=true;
#endif
        break;
    case SetupPhase::Clock:
        title="正在同步时间";detail="已连上网络，正在准备安全连接";break;
    case SetupPhase::ClockError:
        title="时间同步失败";detail="请检查网络，按设备按钮重新配网";break;
    case SetupPhase::Confirming:
        title="正在确认家庭绑定";detail="请保持设备和手机联网";break;
    case SetupPhase::Success:
        title="添加成功";detail="设备已绑定家庭，即将进入机器人";break;
    case SetupPhase::Expired:
        title="配网已超时";detail="请按设备按钮，重新扫码配网";break;
    case SetupPhase::CodeError:
        title="设备码未准备好";detail="请联系设备管理员检查出厂配置";break;
    case SetupPhase::StartError:
        title="配网启动失败";detail="请重启设备后重试";break;
    }
    Board::GetInstance().GetDisplay()->SetOnboardingStatus(title,detail,
        code && screen_code_valid && screen_code.size()==CodeSize?screen_code.data()+48:nullptr,
        code && screen_code_valid && screen_code.size()==CodeSize?CodePixels:0);
}
uint32_t BeginSetup() {
    xSemaphoreTake(lock,portMAX_DELAY);
    const auto epoch=screen_state.Begin();RenderSetup();
    xSemaphoreGive(lock);return epoch;
}
uint32_t CurrentSetup() {
    xSemaphoreTake(lock,portMAX_DELAY);const auto epoch=screen_state.epoch;
    xSemaphoreGive(lock);return epoch;
}
void ShowSetup(SetupPhase phase,uint32_t epoch=0) {
    xSemaphoreTake(lock,portMAX_DELAY);
    if(screen_state.Apply(epoch?epoch:screen_state.epoch,phase))RenderSetup();
    xSemaphoreGive(lock);
}

bool LoadBlob(nvs_handle_t h, const char* key, std::vector<uint8_t>& value, size_t expected) {
    size_t size = 0;
    if (nvs_get_blob(h, key, nullptr, &size) != ESP_OK || size != expected) return false;
    value.resize(size);
    return nvs_get_blob(h, key, value.data(), &size) == ESP_OK;
}
void ClearClaim() {
    xSemaphoreTake(lock, portMAX_DELAY);
    std::fill(challenge.begin(), challenge.end(), '\0'); challenge.clear(); claim_id.clear();
    xSemaphoreGive(lock);
}
bool Token(const cJSON* item, size_t min, size_t max) {
    if (!cJSON_IsString(item)) return false;
    const auto s = item->valuestring; size_t len = strlen(s);
    if (len < min || len > max) return false;
    for (size_t i = 0; i < len; ++i) if (!(isalnum(static_cast<unsigned char>(s[i])) || s[i] == '_' || s[i] == '-')) return false;
    return true;
}
esp_err_t Claim(uint32_t, const uint8_t* input, ssize_t length, uint8_t** output, ssize_t* out_len, void*) {
    if (!running || esp_timer_get_time() >= deadline || length <= 0 || length > 512) return ESP_ERR_INVALID_STATE;
    const char* end = nullptr;
    auto root = cJSON_ParseWithLengthOpts(reinterpret_cast<const char*>(input), length, &end, false);
    bool valid = cJSON_IsObject(root) && end == reinterpret_cast<const char*>(input) + length;
    int count = 0; for (auto item = root ? root->child : nullptr; item; item = item->next) ++count;
    auto id = cJSON_GetObjectItemCaseSensitive(root, "id");
    auto proof = cJSON_GetObjectItemCaseSensitive(root, "challenge");
    valid = valid && count == 2 && Token(id, 16, 128) && Token(proof, 32, 128);
    if (!valid) { cJSON_Delete(root); return ESP_ERR_INVALID_ARG; }
    xSemaphoreTake(lock, portMAX_DELAY);
    // An authenticated retry can replace a cancelled task. The cloud owns
    // cancellation/confirmation races; the worker copies one complete tuple.
    claim_id = id->valuestring; challenge = proof->valuestring;
    xSemaphoreGive(lock); cJSON_Delete(root);
    constexpr char reply[] = "{\"ok\":true}";
    *output = static_cast<uint8_t*>(malloc(sizeof(reply) - 1));
    if (!*output) return ESP_ERR_NO_MEM;
    memcpy(*output, reply, sizeof(reply) - 1); *out_len = sizeof(reply) - 1;
    return ESP_OK; // protocomm encrypts custom endpoint responses after authentication.
}
struct Response { std::string body; bool overflow = false; };
esp_err_t HTTPEvent(esp_http_client_event_t* event) {
    auto response = static_cast<Response*>(event->user_data);
    if (event->event_id == HTTP_EVENT_ON_DATA) {
        if (response->body.size() + event->data_len > 2048) { response->overflow = true; return ESP_FAIL; }
        response->body.append(static_cast<const char*>(event->data), event->data_len);
    }
    return ESP_OK;
}
bool Activate(const std::string& id, const std::string& proof) {
    const bool recover = id.empty();
    auto body = cJSON_CreateObject(); cJSON_AddStringToObject(body, "id", id.c_str()); cJSON_AddStringToObject(body, "challenge", proof.c_str());
    char* encoded = cJSON_PrintUnformatted(body); cJSON_Delete(body); if (!encoded) return false;
    Response response;
    esp_http_client_config_t config{};
    config.url = recover ? Bootstrap : "https://robot.songguoxue.com/api/device/activation";
    config.crt_bundle_attach = esp_crt_bundle_attach;
    config.timeout_ms = 5000; config.disable_auto_redirect = true;
    config.event_handler = HTTPEvent; config.user_data = &response;
    auto http = esp_http_client_init(&config); if (!http) { free(encoded); return false; }
    const auto authorization = "Bearer " + Key();
    esp_http_client_set_header(http, "Authorization", authorization.c_str());
    esp_http_client_set_header(http, "Device-Id", SystemInfo::GetMacAddress().c_str());
    esp_http_client_set_header(http, "Content-Type", "application/json");
    esp_http_client_set_method(http, recover ? HTTP_METHOD_GET : HTTP_METHOD_POST);
    if (!recover) esp_http_client_set_post_field(http, encoded, strlen(encoded));
    auto result = esp_http_client_perform(http); auto status = esp_http_client_get_status_code(http);
    esp_http_client_cleanup(http); memset(encoded, 0, strlen(encoded)); free(encoded);
    if (result != ESP_OK || status != 200 || response.overflow) return false;
    auto root = cJSON_Parse(response.body.c_str());
    auto value = cJSON_GetObjectItemCaseSensitive(root, "status");
    auto task = cJSON_GetObjectItemCaseSensitive(root, "id");
    bool confirmed = cJSON_IsString(value) && strcmp(value->valuestring, "confirmed") == 0 && cJSON_IsString(task) && id == task->valuestring;
    if (recover) {
        auto websocket = cJSON_GetObjectItemCaseSensitive(root, "websocket");
        auto url = cJSON_GetObjectItemCaseSensitive(websocket, "url");
        confirmed = cJSON_IsString(url) && strcmp(url->valuestring, Voice) == 0;
    }
    cJSON_Delete(root); return confirmed;
}
bool SecureClock() {
    if (clock_synced) return true;
    if (esp_netif_sntp_sync_wait(pdMS_TO_TICKS(15000)) == ESP_OK) { clock_synced = true; return true; }
    ESP_LOGE("FamilySetup", "Secure clock unavailable; provisioning remains incomplete");
    Board::GetInstance().GetDisplay()->ShowNotification("时间同步失败，请检查网络后重试");
    return false;
}
bool PostConnected(uint32_t generation) {
    return esp_event_post(FAMILY_CONNECTION_EVENT, ClockReady, &generation, sizeof(generation), pdMS_TO_TICKS(500)) == ESP_OK;
}
void ScheduleClock() {
    if (!VoiceResourcesReady() || !Activated() || !connected || connection_reported || clock_worker.exchange(true)) return;
    if (xTaskCreate([](void*) {
        const auto generation = network_generation.load();
        const bool ready = SecureClock();
        // Only the default event loop may check/mark/notify connection state.
        // Clearing the task guard before posting also permits the next link to retry.
        clock_worker = false;
        const bool posted = ready && PostConnected(generation);
        if (clock_synced && connected && !connection_reported &&
            (!posted || generation != network_generation)) {
            vTaskDelay(pdMS_TO_TICKS(250)); ScheduleClock();
        }
        vTaskDelete(nullptr);
    }, "family_clock", 4096, nullptr, 4, nullptr) != pdPASS) clock_worker = false;
}
void TimeSynchronized(struct timeval*) {
    clock_synced = true;
    ScheduleClock(); // No TLS or application work in the lwIP callback.
}
void Worker(void*) {
    // Reserve the internal stack before BLE; no setup/TLS work until Configure is ready.
    if (ulTaskNotifyTake(pdTRUE,portMAX_DELAY)!=1) {
        worker_active=false;vTaskDelete(nullptr);return;
    }
    const auto epoch=CurrentSetup();
    bool clock_ready = false;
    while (running && esp_timer_get_time() < deadline) {
        if (connected) {
            if (!clock_ready) {
                ShowSetup(SetupPhase::Clock,epoch);
                clock_ready = SecureClock();
                if (!clock_ready) {ShowSetup(SetupPhase::ClockError,epoch);break;}
            }
            if (!running || esp_timer_get_time() >= deadline) break;
            std::string id, proof;
            xSemaphoreTake(lock, portMAX_DELAY); id = claim_id; proof = challenge; xSemaphoreGive(lock);
            ShowSetup(SetupPhase::Confirming,epoch);
            if (Activate(id, proof)) {
                { Settings factory("family_factory", true); factory.SetInt("activated", 1); }
                running = false; esp_timer_stop(expiry_timer); ClearClaim();
                if(!PrepareActivatedVoice()) {
                    ShowSetup(SetupPhase::StartError,epoch);worker_active=false;vTaskDelete(nullptr);return;
                }
                ShowSetup(SetupPhase::Success,epoch);
                vTaskDelay(pdMS_TO_TICKS(2000));
                ShowSetup(SetupPhase::Hidden,epoch);
                if (!PostConnected(network_generation.load())) ScheduleClock();
                worker_active = false; vTaskDelete(nullptr); return;
            }
            std::fill(proof.begin(), proof.end(), '\0');
        }
        vTaskDelay(pdMS_TO_TICKS(1000));
    }
    running = false; esp_timer_stop(expiry_timer); ClearClaim(); network_prov_mgr_stop_provisioning();
    ShowSetup(SetupPhase::Expired,epoch);
    worker_active = false; vTaskDelete(nullptr);
}
void Event(void*, esp_event_base_t base, int32_t event, void* data) {
    if (base == FAMILY_CONNECTION_EVENT && event == ClockReady) {
        const auto generation = *static_cast<const uint32_t*>(data);
        if (VoiceResourcesReady() && generation == network_generation && connected && Activated() && clock_synced &&
            !connection_reported.exchange(true) && callback) callback(true);
        return;
    }
    if (base == NETWORK_PROV_EVENT && event == NETWORK_PROV_WIFI_CRED_RECV && running)
        ShowSetup(SetupPhase::Connecting);
    if (base == NETWORK_PROV_EVENT && event == NETWORK_PROV_WIFI_CRED_FAIL && running) {
        network_prov_mgr_reset_wifi_sm_state_on_failure();
        Board::GetInstance().GetDisplay()->ShowNotification("Wi-Fi 连接失败，请重新输入密码");
        ShowSetup(SetupPhase::WifiError);
    }
    if (base == WIFI_EVENT && event == WIFI_EVENT_STA_START) esp_wifi_connect();
    if (base == WIFI_EVENT && event == WIFI_EVENT_STA_DISCONNECTED) {
        connected = false; ++network_generation; connection_reported = false;
        if (Activated()) { if (callback) callback(false); esp_wifi_connect(); }
    }
    if (base == IP_EVENT && event == IP_EVENT_STA_GOT_IP) {
        if (!clock_synced) esp_netif_sntp_start();
        connected = true;
        if (running) ShowSetup(SetupPhase::Clock);
        ScheduleClock();
    }
}
}
void ReportConfigureStartup() {
    ConfigureStartupSnapshot snapshot;
    portENTER_CRITICAL(&configure_startup_mux);
    const bool report=configure_startup_pending;
    if(report) {
        snapshot=configure_startup;
        // Early USB output can be lost: repeat only the fixed cleanup fact in existing heap telemetry.
        configure_startup_pending=snapshot.resource_stage!=0;
    }
    portEXIT_CRITICAL(&configure_startup_mux);
    if(report) ESP_LOGI("FamilySetup","configure replay stage=%s err=%d internal=%u largest=%u free8=%u reclaimed=%d resource_stage=%u prior_internal=%u",
        snapshot.stage,snapshot.result,snapshot.internal,snapshot.largest,snapshot.free8,snapshot.reclaimed,snapshot.resource_stage,snapshot.prior_internal);
}
bool PrepareVoiceOnBoot() {
    return !Present() || !Activated() || PrepareActivatedVoice();
}
bool Present() {
    nvs_handle_t h; const auto err = nvs_open("family_factory", NVS_READONLY, &h);
    if (err == ESP_ERR_NVS_NOT_FOUND) return false;
    if (err == ESP_OK) nvs_close(h);
    return true; // Read/corruption errors must not select the legacy cloud path.
}
bool Activated() { return Settings("family_factory").GetInt("activated") == 1; }
std::string Key() { return Settings("family_factory").GetString("key"); }
void Configure() {
    if (!initialized || running || worker_active || Activated()) return;
    LogConfigure("begin",ESP_OK);
    BeginSetup();
    LogConfigure("screen",ESP_OK);
    if (!manager_ready) {
        network_prov_mgr_config_t config{}; config.scheme = network_prov_scheme_ble;
        config.scheme_event_handler = NETWORK_PROV_EVENT_HANDLER_NONE;
        config.network_prov_wifi_conn_cfg.wifi_conn_attempts = 3;
        const auto result=network_prov_mgr_init(config);LogConfigure("manager_init",result);
        if (result != ESP_OK) {ShowSetup(SetupPhase::StartError);return;}
        manager_ready = true;
    }
    ClearClaim();
    if (!endpoint_created) {
        const auto result=network_prov_mgr_endpoint_create("family-claim");LogConfigure("endpoint_create",result);
        if (result != ESP_OK) {ShowSetup(SetupPhase::StartError);return;}
        endpoint_created = true;
    }
    auto result=network_prov_mgr_disable_auto_stop(1000);LogConfigure("auto_stop",result);
    if (result != ESP_OK) {ShowSetup(SetupPhase::StartError);return;}
    security.salt = reinterpret_cast<const char*>(salt.data()); security.salt_len = salt.size();
    security.verifier = reinterpret_cast<const char*>(verifier.data()); security.verifier_len = verifier.size();
    auto mac = SystemInfo::GetMacAddress(); std::string name = "FAMILY_";
    for (char c : mac) if (c != ':') name += c;
    TaskHandle_t worker=nullptr;
    worker_active=true;
    const auto task_result=xTaskCreate(Worker,"family_claim",8192,nullptr,4,&worker);
    LogConfigure("worker_create",task_result==pdPASS?ESP_OK:static_cast<int>(task_result));
    if(task_result!=pdPASS) {worker_active=false;ShowSetup(SetupPhase::StartError);return;}
    deadline = esp_timer_get_time() + WindowUs; running = true;
    result=network_prov_mgr_start_provisioning(NETWORK_PROV_SECURITY_2, &security, name.c_str(), nullptr);
    LogConfigure("provision_start",result);
    if (result==ESP_OK) {
        result=network_prov_mgr_endpoint_register("family-claim", Claim, nullptr);
        LogConfigure("endpoint_register",result);
    }
    if(result==ESP_OK) {
        result=esp_timer_start_once(expiry_timer,WindowUs);
        LogConfigure("expiry_start",result);
    }
    if (result != ESP_OK) {
        running=false;network_prov_mgr_stop_provisioning();ShowSetup(SetupPhase::StartError);
        // Keep the retry guard until caller cleanup is complete; cancellation skips Worker cleanup.
        xTaskNotify(worker,2,eSetValueWithOverwrite);return;
    }
    xTaskNotify(worker,1,eSetValueWithOverwrite);
}
void Start(std::function<void(bool)> connection) {
    if (initialized) return;
    callback = std::move(connection);
    nvs_handle_t h; if (nvs_open("family_factory", NVS_READONLY, &h) != ESP_OK) return;
    const bool valid = LoadBlob(h, "salt", salt, 16) && LoadBlob(h, "verifier", verifier, 384);
    bool screen_ready=true;
#if CONFIG_BOARD_TYPE_WAVESHARE_ESP32_S3_TOUCH_AMOLED_2_16 || CONFIG_BOARD_TYPE_M5STACK_STOPWATCH
    if (!Activated()) screen_ready=LoadBlob(h,"screen_code",screen_code,CodeSize) &&
        ValidCode(screen_code.data(),screen_code.size(),Settings("family_factory").GetString("device_id"));
    screen_code_valid=screen_ready && screen_code.size()==CodeSize;
    if (!screen_code_valid)screen_code.clear();
#endif
    nvs_close(h); const auto key = Key();
    if (!valid || key.size() != 43 || Settings("family_factory").GetString("mac") != SystemInfo::GetMacAddress()) return;
    lock = xSemaphoreCreateMutex(); if (!lock) return;
    if (!screen_ready) {BeginSetup();ShowSetup(SetupPhase::CodeError);return;}
    if(Activated() && !PrepareActivatedVoice()) {BeginSetup();ShowSetup(SetupPhase::StartError);return;}
    auto err = esp_netif_init(); if (err != ESP_OK) return;
    err = esp_event_loop_create_default(); if (err != ESP_OK && err != ESP_ERR_INVALID_STATE) return;
    if (!esp_netif_create_default_wifi_sta()) return;
    wifi_init_config_t config = WIFI_INIT_CONFIG_DEFAULT();
    if (esp_wifi_init(&config) != ESP_OK || esp_wifi_set_mode(WIFI_MODE_STA) != ESP_OK) return;
    if (esp_event_handler_register(FAMILY_CONNECTION_EVENT, ClockReady, Event, nullptr) != ESP_OK ||
        esp_event_handler_register(NETWORK_PROV_EVENT, ESP_EVENT_ANY_ID, Event, nullptr) != ESP_OK ||
        esp_event_handler_register(WIFI_EVENT, ESP_EVENT_ANY_ID, Event, nullptr) != ESP_OK ||
        esp_event_handler_register(IP_EVENT, IP_EVENT_STA_GOT_IP, Event, nullptr) != ESP_OK) return;
    esp_timer_create_args_t timer{};
    timer.callback = [](void*) { running = false; network_prov_mgr_stop_provisioning(); };
    timer.name = "family_expiry";
    if (esp_timer_create(&timer, &expiry_timer) != ESP_OK) return;
    esp_sntp_config_t sntp = ESP_NETIF_SNTP_DEFAULT_CONFIG("pool.ntp.org");
    sntp.sync_cb = TimeSynchronized;
    sntp.start = false; // Start after IP, avoiding pre-Wi-Fi retry backoff.
    if (esp_netif_sntp_init(&sntp) != ESP_OK) return;
    initialized = true;
    wifi_config_t saved{};
    if (Activated()) { esp_wifi_start(); return; }
    if (esp_wifi_get_config(WIFI_IF_STA, &saved) == ESP_OK && saved.sta.ssid[0]) {
        // A crash may happen after cloud confirmation but before the local NVS
        // receipt. Try authenticated bootstrap on saved Wi-Fi before the official
        // provisioning manager clears RAM credentials for its new session.
        BeginSetup();ShowSetup(SetupPhase::Connecting);
        esp_wifi_start(); worker_active = true;
        if (xTaskCreate([](void*) {
            const auto epoch=CurrentSetup();
            const auto until = esp_timer_get_time() + 20000000;
            while (!connected && esp_timer_get_time() < until) vTaskDelay(pdMS_TO_TICKS(250));
            if (connected)ShowSetup(SetupPhase::Clock,epoch);
            const bool ready=connected && SecureClock();
            if (ready)ShowSetup(SetupPhase::Confirming,epoch);
            if (ready && Activate("", "")) {
                { Settings factory("family_factory", true); factory.SetInt("activated", 1); }
                if(!PrepareActivatedVoice()) {
                    ShowSetup(SetupPhase::StartError,epoch);worker_active=false;vTaskDelete(nullptr);return;
                }
                ShowSetup(SetupPhase::Success,epoch);vTaskDelay(pdMS_TO_TICKS(2000));
                ShowSetup(SetupPhase::Hidden,epoch);
                worker_active = false; if (!PostConnected(network_generation.load())) ScheduleClock();
            } else {
                esp_wifi_disconnect(); esp_wifi_stop(); connected = false; worker_active = false;
                Configure();
            }
            vTaskDelete(nullptr);
        }, "family_recover", 8192, nullptr, 4, nullptr) != pdPASS) {
            worker_active = false; esp_wifi_stop(); Configure();
        }
    } else Configure();
}
}

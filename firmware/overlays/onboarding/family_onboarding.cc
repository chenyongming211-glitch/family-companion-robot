#include "family_onboarding.h"
#include "settings.h"
#include "system_info.h"
#include <esp_crt_bundle.h>
#include <esp_event.h>
#include <esp_http_client.h>
#include <esp_netif.h>
#include <esp_netif_sntp.h>
#include <esp_log.h>
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
constexpr int64_t WindowUs = 180LL * 1000000;
std::function<void(bool)> callback;
SemaphoreHandle_t lock;
std::string claim_id, challenge;
std::vector<uint8_t> salt, verifier;
network_prov_security2_params_t security{};
std::atomic<bool> connected{false}, running{false}, worker_active{false}, clock_worker{false}, clock_synced{false}, connection_reported{false};
std::atomic<uint32_t> network_generation{0};
std::atomic<int64_t> deadline{0};
bool initialized = false;
bool manager_ready = false;
bool endpoint_created = false;
esp_timer_handle_t expiry_timer = nullptr;

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
void ScheduleClock() {
    if (!Activated() || !connected || connection_reported || clock_worker.exchange(true)) return;
    if (xTaskCreate([](void*) {
        const auto generation = network_generation.load();
        if (SecureClock() && connected && generation == network_generation && callback &&
            !connection_reported.exchange(true)) callback(true);
        clock_worker = false;
        // Sync can arrive after the bounded wait times out but before this task
        // clears its guard. Recheck after clearing so that notification is not lost.
        if (clock_synced && connected && !connection_reported) ScheduleClock();
        vTaskDelete(nullptr);
    }, "family_clock", 4096, nullptr, 4, nullptr) != pdPASS) clock_worker = false;
}
void TimeSynchronized(struct timeval*) {
    clock_synced = true;
    ScheduleClock(); // No TLS or application work in the lwIP callback.
}
void Worker(void*) {
    bool clock_ready = false;
    while (running && esp_timer_get_time() < deadline) {
        if (connected) {
            if (!clock_ready) {
                clock_ready = SecureClock();
                if (!clock_ready) break;
            }
            if (!running || esp_timer_get_time() >= deadline) break;
            std::string id, proof;
            xSemaphoreTake(lock, portMAX_DELAY); id = claim_id; proof = challenge; xSemaphoreGive(lock);
            if (Activate(id, proof)) {
                { Settings factory("family_factory", true); factory.SetInt("activated", 1); }
                running = false; esp_timer_stop(expiry_timer); ClearClaim(); network_prov_mgr_stop_provisioning();
                if (callback) callback(true);
                worker_active = false; vTaskDelete(nullptr); return;
            }
            std::fill(proof.begin(), proof.end(), '\0');
        }
        vTaskDelay(pdMS_TO_TICKS(1000));
    }
    running = false; esp_timer_stop(expiry_timer); ClearClaim(); network_prov_mgr_stop_provisioning();
    worker_active = false; vTaskDelete(nullptr);
}
void Event(void*, esp_event_base_t base, int32_t event, void*) {
    if (base == NETWORK_PROV_EVENT && event == NETWORK_PROV_WIFI_CRED_FAIL && running) {
        network_prov_mgr_reset_wifi_sm_state_on_failure();
        Board::GetInstance().GetDisplay()->ShowNotification("Wi-Fi 连接失败，请重新输入密码");
    }
    if (base == WIFI_EVENT && event == WIFI_EVENT_STA_START) esp_wifi_connect();
    if (base == WIFI_EVENT && event == WIFI_EVENT_STA_DISCONNECTED) {
        connected = false; ++network_generation; connection_reported = false;
        if (Activated()) { if (callback) callback(false); esp_wifi_connect(); }
    }
    if (base == IP_EVENT && event == IP_EVENT_STA_GOT_IP) {
        if (!clock_synced) esp_netif_sntp_start();
        connected = true;
        ScheduleClock();
    }
}
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
    if (!manager_ready) {
        network_prov_mgr_config_t config{}; config.scheme = network_prov_scheme_ble;
        config.scheme_event_handler = NETWORK_PROV_EVENT_HANDLER_NONE;
        config.network_prov_wifi_conn_cfg.wifi_conn_attempts = 3;
        if (network_prov_mgr_init(config) != ESP_OK) return;
        manager_ready = true;
    }
    ClearClaim();
    if (!endpoint_created) {
        if (network_prov_mgr_endpoint_create("family-claim") != ESP_OK) return;
        endpoint_created = true;
    }
    if (network_prov_mgr_disable_auto_stop(1000) != ESP_OK) return;
    security.salt = reinterpret_cast<const char*>(salt.data()); security.salt_len = salt.size();
    security.verifier = reinterpret_cast<const char*>(verifier.data()); security.verifier_len = verifier.size();
    auto mac = SystemInfo::GetMacAddress(); std::string name = "FAMILY_";
    for (char c : mac) if (c != ':') name += c;
    deadline = esp_timer_get_time() + WindowUs; running = true;
    if (network_prov_mgr_start_provisioning(NETWORK_PROV_SECURITY_2, &security, name.c_str(), nullptr) != ESP_OK ||
        network_prov_mgr_endpoint_register("family-claim", Claim, nullptr) != ESP_OK) {
        running = false; network_prov_mgr_stop_provisioning(); return;
    }
    esp_timer_start_once(expiry_timer, WindowUs);
    worker_active = true;
    if (xTaskCreate(Worker, "family_claim", 8192, nullptr, 4, nullptr) != pdPASS) {
        worker_active = false; esp_timer_stop(expiry_timer); running = false; network_prov_mgr_stop_provisioning();
    }
}
void Start(std::function<void(bool)> connection) {
    if (initialized) return;
    callback = std::move(connection);
    nvs_handle_t h; if (nvs_open("family_factory", NVS_READONLY, &h) != ESP_OK) return;
    const bool valid = LoadBlob(h, "salt", salt, 16) && LoadBlob(h, "verifier", verifier, 384);
    nvs_close(h); const auto key = Key();
    if (!valid || key.size() != 43 || Settings("family_factory").GetString("mac") != SystemInfo::GetMacAddress()) return;
    lock = xSemaphoreCreateMutex(); if (!lock) return;
    auto err = esp_netif_init(); if (err != ESP_OK) return;
    err = esp_event_loop_create_default(); if (err != ESP_OK && err != ESP_ERR_INVALID_STATE) return;
    if (!esp_netif_create_default_wifi_sta()) return;
    wifi_init_config_t config = WIFI_INIT_CONFIG_DEFAULT();
    if (esp_wifi_init(&config) != ESP_OK || esp_wifi_set_mode(WIFI_MODE_STA) != ESP_OK) return;
    if (esp_event_handler_register(NETWORK_PROV_EVENT, NETWORK_PROV_WIFI_CRED_FAIL, Event, nullptr) != ESP_OK ||
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
        esp_wifi_start(); worker_active = true;
        if (xTaskCreate([](void*) {
            const auto until = esp_timer_get_time() + 20000000;
            while (!connected && esp_timer_get_time() < until) vTaskDelay(pdMS_TO_TICKS(250));
            if (connected && SecureClock() && Activate("", "")) {
                { Settings factory("family_factory", true); factory.SetInt("activated", 1); }
                worker_active = false; if (callback) callback(true);
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

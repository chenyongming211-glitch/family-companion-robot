#include "family_ota_protocol.h"
#include "family_onboarding.h"
#include "family_control_state.h"
#include "application.h"
#include "audio_codec.h"
#include "board.h"
#include "system_info.h"
#include <atomic>
#include <algorithm>
#include <mutex>
#include <new>
#include <sys/socket.h>
#include <esp_app_desc.h>
#include <esp_crt_bundle.h>
#include <esp_http_client.h>
#include <esp_heap_caps.h>
#include <esp_ota_ops.h>
#include <esp_timer.h>
#include <esp_system.h>
#include <psa/crypto.h>
#include <freertos/FreeRTOS.h>
#include <freertos/task.h>
namespace {
uint64_t Now(){return static_cast<uint64_t>(esp_timer_get_time()/1000);}
bool CompatibleLayout() {
    struct Expected{const char* name;esp_partition_type_t type;esp_partition_subtype_t subtype;uint32_t offset,size;};
    const Expected expected[]={
        {"nvs",ESP_PARTITION_TYPE_DATA,ESP_PARTITION_SUBTYPE_DATA_NVS,0x9000,0x6000},{"otadata",ESP_PARTITION_TYPE_DATA,ESP_PARTITION_SUBTYPE_DATA_OTA,0xf000,0x2000},
        {"phy_init",ESP_PARTITION_TYPE_DATA,ESP_PARTITION_SUBTYPE_DATA_PHY,0x11000,0x1000},{"ota_0",ESP_PARTITION_TYPE_APP,ESP_PARTITION_SUBTYPE_APP_OTA_0,0x20000,0x370000},
        {"ota_1",ESP_PARTITION_TYPE_APP,ESP_PARTITION_SUBTYPE_APP_OTA_1,0x390000,0x370000},{"assets",ESP_PARTITION_TYPE_DATA,ESP_PARTITION_SUBTYPE_DATA_SPIFFS,0x700000,0x600000},
        {"storage",ESP_PARTITION_TYPE_DATA,ESP_PARTITION_SUBTYPE_DATA_SPIFFS,0xd00000,0x300000}};
    for(const auto& e:expected){auto p=esp_partition_find_first(e.type,e.subtype,e.name);if(!p||p->address!=e.offset||p->size!=e.size)return false;}
    const auto running=esp_ota_get_running_partition();return running&&(running->address==0x20000||running->address==0x390000)&&running->size==FamilyOta::SlotBytes;
}
}
namespace FamilyOta {namespace {void Progress(int value);}const char* Download(const Command& command);bool DownloadAvailable();}
#include "family_ota_runtime.inc"
#include "family_ota_download.inc"

#pragma once
#include "audio_fault.h"
#include <nvs.h>

namespace family_robot {
// Task context only, serialized with audio recovery by the adapter. No erase or
// abort: unavailable/full/corrupt NVS must leave audio fault isolation intact.
inline bool LoadAudioFaultSnapshot(AudioFaultSnapshot& snapshot) {
    nvs_handle_t handle;
    if(nvs_open("audiofault",NVS_READONLY,&handle)!=ESP_OK) return false;
    AudioFaultSnapshot loaded{};
    size_t size=sizeof(loaded);
    const auto result=nvs_get_blob(handle,"first",&loaded,&size);
    nvs_close(handle);
    if(result!=ESP_OK || size!=sizeof(loaded) || !ValidateAudioFaultSnapshot(loaded))
        return false;
    snapshot=loaded;
    return true;
}
inline bool SaveAudioFaultSnapshot(const AudioFaultSnapshot& snapshot) {
    if(!ValidateAudioFaultSnapshot(snapshot)) return false;
    nvs_handle_t handle;
    if(nvs_open("audiofault",NVS_READWRITE,&handle)!=ESP_OK) return false;
    auto result=nvs_set_blob(handle,"first",&snapshot,sizeof(snapshot));
    if(result==ESP_OK) result=nvs_commit(handle);
    nvs_close(handle);
    return result==ESP_OK;
}
} // namespace family_robot

#pragma once
#include "wave_wake_probe.h"
#include <freertos/FreeRTOS.h>
#include <esp_timer.h>
#include <cstdio>
namespace wave_wake_lab {
inline portMUX_TYPE mux=portMUX_INITIALIZER_UNLOCKED;
inline Window window;
inline void Record(unsigned channel,const int16_t* pcm,size_t frames,size_t stride) {
    if(!pcm||!frames||frames>1024)return;
    portENTER_CRITICAL(&mux);const auto token=window.Token(esp_timer_get_time());portEXIT_CRITICAL(&mux);
    if(!token)return;
    Stats s;s.Add(pcm,frames,stride);
    portENTER_CRITICAL(&mux);window.Merge(token,channel,s);portEXIT_CRITICAL(&mux);
}
inline void Raw(const int16_t* pcm,size_t samples) {if(samples%2==0){Record(0,pcm,samples/2,2);Record(1,pcm+1,samples/2,2);}}
inline void Processed(const int16_t* pcm,size_t samples) {Record(2,pcm,samples,1);}
inline bool Begin(unsigned seconds) {
    portENTER_CRITICAL(&mux);const bool ok=window.Begin(esp_timer_get_time(),seconds);portEXIT_CRITICAL(&mux);return ok;
}
inline void Flush() {
    portENTER_CRITICAL(&mux);const auto s=window.Take(esp_timer_get_time());portEXIT_CRITICAL(&mux);
    if(!s.id)return;
    for(unsigned i=0;i<s.channels.size();++i) {
        const auto& c=s.channels[i];
        printf("WWD_LEVEL id=%u ch=%u samples=%u rms=%u ac=%u peak=%u clipped=%u done=%d\n",
            unsigned(s.id),i,unsigned(c.samples),unsigned(std::lround(c.Rms())),unsigned(std::lround(c.AcRms())),unsigned(c.peak),unsigned(c.clipped),s.done);
    }
    fflush(stdout);
}
}

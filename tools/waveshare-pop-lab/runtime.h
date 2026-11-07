#pragma once
#include "probe.h"
#include <algorithm>
#include <atomic>
#include <cstddef>
#include <cstring>
#include <freertos/FreeRTOS.h>
#include <freertos/portmacro.h>
#include <esp_timer.h>
namespace wave_pop_lab {
// Xtensa's std::atomic RMW is not always lock-free. Use one short portMUX with
// an explicit ISR reader; no atomics library or task-only lock runs in the ISR.
inline portMUX_TYPE metrics_mux=portMUX_INITIALIZER_UNLOCKED;
template<class T> struct Scalar {
    T value{};
    T load(std::memory_order = std::memory_order_seq_cst) const {
        portENTER_CRITICAL(&metrics_mux);const auto result=value;portEXIT_CRITICAL(&metrics_mux);return result;
    }
    void store(T v,std::memory_order = std::memory_order_seq_cst) {
        portENTER_CRITICAL(&metrics_mux);value=v;portEXIT_CRITICAL(&metrics_mux);
    }
};
inline Probe probe;
inline Scalar<bool> active{false}, capturing{false}, input_busy{false};
inline Scalar<uint32_t> decode_backlog{0}, pcm_backlog{0}, max_decode_us{0}, max_wait_us{0}, captured{0};
inline Scalar<uint32_t> decode_inflight{0}, pcm_inflight{0};
struct Supply {bool active;uint32_t decode,pcm;};
inline Supply ReadSupplyISR() {
    portENTER_CRITICAL_ISR(&metrics_mux);
    const Supply s{active.value,decode_backlog.value+decode_inflight.value,pcm_backlog.value+pcm_inflight.value};
    portEXIT_CRITICAL_ISR(&metrics_mux);return s;
}
inline int16_t* capture=nullptr;
inline uint32_t* capture_times=nullptr;
inline Scalar<uint32_t> time_count{0};
inline constexpr size_t kTimeEntries=2000;
inline constexpr size_t kCaptureSamples=24000*2*10;
inline void Max(Scalar<uint32_t>& target,uint32_t value) {
    portENTER_CRITICAL(&metrics_mux);if(target.value<value)target.value=value;portEXIT_CRITICAL(&metrics_mux);
}
inline void Input(const int16_t* data,size_t samples) {
    input_busy.store(true,std::memory_order_release);
    if(capturing.load(std::memory_order_acquire) && capture) {
        const size_t used=captured.load(std::memory_order_relaxed);
        const size_t count=std::min(samples,kCaptureSamples-used);
        const auto entry=time_count.load();
        if(count && entry<kTimeEntries) {
            capture_times[entry*2]=used;capture_times[entry*2+1]=uint32_t(esp_timer_get_time());
            time_count.store(entry+1);
        }
        std::memcpy(capture+used,data,count*sizeof(int16_t));
        captured.store(used+count,std::memory_order_release);
    }
    input_busy.store(false,std::memory_order_release);
}
}

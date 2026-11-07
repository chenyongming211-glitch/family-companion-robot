#pragma once
#include "probe.h"
#include <algorithm>
#include <atomic>
#include <cstddef>
#include <cstring>
namespace wave_pop_lab {
static_assert(std::atomic<uint32_t>::is_always_lock_free);
inline Probe probe;
inline std::atomic<bool> active{false}, capturing{false}, input_busy{false};
inline std::atomic<uint32_t> decode_backlog{0}, pcm_backlog{0}, max_decode_us{0}, max_wait_us{0}, captured{0};
inline std::atomic<uint32_t> decode_inflight{0}, pcm_inflight{0};
inline int16_t* capture=nullptr;
inline constexpr size_t kCaptureSamples=24000*2*10;
inline void Max(std::atomic<uint32_t>& target,uint32_t value) {
    auto old=target.load(std::memory_order_relaxed);
    while(old<value && !target.compare_exchange_weak(old,value,std::memory_order_relaxed)) {}
}
inline void Input(const int16_t* data,size_t samples) {
    input_busy.store(true,std::memory_order_release);
    if(capturing.load(std::memory_order_acquire) && capture) {
        const size_t used=captured.load(std::memory_order_relaxed);
        const size_t count=std::min(samples,kCaptureSamples-used);
        std::memcpy(capture+used,data,count*sizeof(int16_t));
        captured.store(used+count,std::memory_order_release);
    }
    input_busy.store(false,std::memory_order_release);
}
}

#include "family_c3_codec.h"
#if CONFIG_XMINI_C3_BUTTON_ENTRY
#include <driver/i2s_std.h>
#include <esp_heap_caps.h>
#include <esp_timer.h>
#include <algorithm>
#include <cstring>
#include <exception>
#include <new>
#if CONFIG_XMINI_C3_PCM
#if !CONFIG_I2S_ISR_IRAM_SAFE
#error "C3 PCM requires a cache-safe I2S interrupt"
#endif
#include "c3_cache_trace.h"
#else
#if CONFIG_I2S_ISR_IRAM_SAFE
#error "Legacy C3 playback callback graph requires I2S_ISR_IRAM_SAFE disabled"
#endif
#endif
#include "family_c3_lifecycle.inc"
#include "family_c3_playback.inc"
#if !CONFIG_XMINI_C3_PCM
#include "family_c3_isr.inc"
#endif
#if CONFIG_XMINI_C3_VOICE_BARGE
#include "family_c3_capture.inc"
#endif
#endif

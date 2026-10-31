#include "family_c3_codec.h"
#if CONFIG_XMINI_C3_BUTTON_ENTRY
#include <driver/i2s_std.h>
#include <esp_heap_caps.h>
#include <esp_timer.h>
#include <algorithm>
#include <cstring>
#include <exception>
#include <new>
#if CONFIG_I2S_ISR_IRAM_SAFE
#error "C3 playback callback graph requires I2S_ISR_IRAM_SAFE disabled"
#endif
#include "family_c3_lifecycle.inc"
#include "family_c3_playback.inc"
#include "family_c3_dma.inc"
#if CONFIG_XMINI_C3_VOICE_BARGE
#include "family_c3_capture.inc"
#endif
#endif

#include "xiaoman_box_codec.h"
#if CONFIG_WAVESHARE_XIAOMAN_REALTIME
#include <driver/i2s_tdm.h>
#include <esp_heap_caps.h>
#include <esp_timer.h>
#include <algorithm>
#include <cstring>
#include <new>
#if CONFIG_I2S_ISR_IRAM_SAFE
#error "Waveshare callback graph requires I2S_ISR_IRAM_SAFE disabled"
#endif
#include "xiaoman_box_lifecycle.inc"
#include "xiaoman_box_playback.inc"
#include "xiaoman_box_dma.inc"
#endif

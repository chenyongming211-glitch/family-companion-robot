#pragma once
#include "FreeRTOS.h"
using TaskHandle_t = void*;
// Deterministic send/connect harness: no receive task is scheduled. The separate
// WebSocket harness checks owner/callback teardown ordering through public APIs.
inline BaseType_t xTaskCreate(void (*)(void*), const char*, uint32_t, void*, int, TaskHandle_t*) { return pdPASS; }
inline void vTaskDelete(void*) {}

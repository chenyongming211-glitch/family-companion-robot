#pragma once
#include <cstdint>
using EventBits_t = uint32_t;
using TickType_t = uint32_t;
using BaseType_t = int;
#define BIT0 1U
#define BIT1 2U
#define pdFALSE 0
#define pdTRUE 1
#define pdPASS 1
#define portMAX_DELAY UINT32_MAX
#define portTICK_PERIOD_MS 1
#define pdMS_TO_TICKS(x) (x)

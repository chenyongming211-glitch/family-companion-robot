#pragma once
#include <cstdint>
using TickType_t=uint32_t;
using EventBits_t=uint32_t;
constexpr int pdFALSE=0,pdTRUE=1,portTICK_PERIOD_MS=1;
constexpr EventBits_t BIT0=1,BIT1=2;
#define pdMS_TO_TICKS(ms) (ms)

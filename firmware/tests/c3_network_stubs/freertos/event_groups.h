#pragma once
#include "FreeRTOS.h"
#include <atomic>
struct TestEventGroup { std::atomic<EventBits_t> bits{0}; };
using EventGroupHandle_t = TestEventGroup*;
extern int test_event_deletes;
inline EventGroupHandle_t xEventGroupCreate() { return new TestEventGroup; }
inline void vEventGroupDelete(EventGroupHandle_t g) { ++test_event_deletes; delete g; }
inline EventBits_t xEventGroupGetBits(EventGroupHandle_t g) { return g->bits.load(); }
inline EventBits_t xEventGroupSetBits(EventGroupHandle_t g, EventBits_t b) { return g->bits.fetch_or(b) | b; }
inline EventBits_t xEventGroupClearBits(EventGroupHandle_t g, EventBits_t b) { return g->bits.fetch_and(~b); }
inline EventBits_t xEventGroupWaitBits(EventGroupHandle_t g, EventBits_t, int, int, TickType_t) { return g->bits.load(); }
inline TickType_t xTaskGetTickCount() { return 0; }

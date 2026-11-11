#pragma once
#include "FreeRTOS.h"
#include <atomic>
struct DiagnosticEventGroup{std::atomic<EventBits_t> bits{0};};
using EventGroupHandle_t=DiagnosticEventGroup*;
inline EventGroupHandle_t xEventGroupCreate(){return new DiagnosticEventGroup;}
inline void vEventGroupDelete(EventGroupHandle_t group){delete group;}
inline EventBits_t xEventGroupClearBits(EventGroupHandle_t group,EventBits_t bits){return group->bits.fetch_and(~bits);}
inline EventBits_t xEventGroupSetBits(EventGroupHandle_t group,EventBits_t bits){return group->bits.fetch_or(bits)|bits;}
// No real RTOS delay: an absent response deterministically models the existing
// wait expiring. This is not a measured ten-second timeout or a real outage.
inline EventBits_t xEventGroupWaitBits(EventGroupHandle_t group,EventBits_t,int,int,TickType_t){return group->bits.load();}
inline TickType_t xTaskGetTickCount(){return 0;}

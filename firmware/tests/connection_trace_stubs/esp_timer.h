#pragma once
#include <cstdint>
extern int64_t trace_test_us;
inline int64_t esp_timer_get_time() { return trace_test_us; }

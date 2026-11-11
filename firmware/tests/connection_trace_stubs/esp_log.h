#pragma once
void trace_test_log(const char*, const char*, ...);
#define ESP_LOGI(tag, format, ...) trace_test_log(tag, format, ##__VA_ARGS__)
#define ESP_LOGE(tag, format, ...) trace_test_log(tag, format, ##__VA_ARGS__)
#define ESP_LOGD(tag, format, ...) trace_test_log(tag, format, ##__VA_ARGS__)

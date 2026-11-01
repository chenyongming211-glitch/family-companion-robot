#pragma once
template<class... Args> inline void TestLog(const char*, const char*, Args...) {}
#define ESP_LOGE(...) TestLog(__VA_ARGS__)
#define ESP_LOGI(...) TestLog(__VA_ARGS__)
#define ESP_LOGD(...) TestLog(__VA_ARGS__)

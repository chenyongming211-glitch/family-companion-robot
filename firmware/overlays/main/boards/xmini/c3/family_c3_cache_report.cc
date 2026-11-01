#include "sdkconfig.h"
#if CONFIG_XMINI_C3_PCM
#include "c3_cache_trace.h"
#include <esp_log.h>
void family_c3_cache_report() {
    ESP_LOGI("C3Cache","slots=%u bytes=%u overflow=%u clock=esp_timer_get_time",
        family_robot::C3CacheSpan::kSlots,unsigned(sizeof(family_robot::C3CacheSpan)),unsigned(family_c3_cache_overflowed()));
    family_robot::C3CacheSpan::Record peak;
    if(family_c3_cache_peak(peak)) {
        ESP_LOGI("C3Cache","longest_observed_completed gen=%lu proven=%u enter_before=%lld disable_after=%lld enable_before=%lld enable_after=%lld rx=%lu tx=%lu conservative_cache_off_us=%lld",
            (unsigned long)peak.generation,unsigned(peak.proven),(long long)peak.enter_before,(long long)peak.disable_after,
            (long long)peak.enable_before,(long long)peak.enable_after,(unsigned long)peak.rx,(unsigned long)peak.tx,
            (long long)(peak.enable_before-peak.disable_after));
    }
    const auto used=family_c3_cache_slots_used();
    for(unsigned i=0;i<used && i<family_robot::C3CacheSpan::kSlots;++i) {
        family_robot::C3CacheSpan::Record r;
        if(!family_c3_cache_read(i,r)) {
            ESP_LOGI("C3Cache","slot=%u observed=0 proven=0 incomplete_or_busy=1",i);continue;
        }
        ESP_LOGI("C3Cache","slot=%u gen=%lu proven=%u enter_before=%lld disable_after=%lld enable_before=%lld enable_after=%lld rx=%lu tx=%lu conservative_cache_off_us=%lld",
            i,(unsigned long)r.generation,unsigned(r.proven),(long long)r.enter_before,(long long)r.disable_after,
            (long long)r.enable_before,(long long)r.enable_after,(unsigned long)r.rx,(unsigned long)r.tx,
            r.proven?(long long)(r.enable_before-r.disable_after):-1LL);
    }
}
#endif

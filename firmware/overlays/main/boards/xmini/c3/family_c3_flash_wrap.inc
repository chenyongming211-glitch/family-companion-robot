extern "C" void __real_spi_flash_disable_interrupts_caches_and_other_cpu();
extern "C" void __real_spi_flash_enable_interrupts_caches_and_other_cpu();
extern "C" void __wrap_spi_flash_disable_interrupts_caches_and_other_cpu() {
    // Earliest boot is an entirely transparent pass-through: no timer access.
    if(!family_c3_cache_trace.enabled()) {__real_spi_flash_disable_interrupts_caches_and_other_cpu();return;}
    auto token=family_c3_cache_trace.EnterBefore(esp_timer_get_time());
    __real_spi_flash_disable_interrupts_caches_and_other_cpu();
    family_c3_cache_trace.DisableAfter(token,esp_timer_get_time());
}
extern "C" void __wrap_spi_flash_enable_interrupts_caches_and_other_cpu() {
    if(!family_c3_cache_trace.enabled()) {__real_spi_flash_enable_interrupts_caches_and_other_cpu();return;}
    const auto token=family_c3_cache_trace.BeginEnable();
    family_c3_cache_trace.EnableBefore(token,esp_timer_get_time());
    __real_spi_flash_enable_interrupts_caches_and_other_cpu();
    // Keep the ORIGINAL token on this stack. Scheduling can resume inside real
    // enable and a different task can finish another Flash interval first.
    family_c3_cache_trace.EnableAfter(token,esp_timer_get_time());
}

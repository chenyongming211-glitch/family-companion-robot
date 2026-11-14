// Flash/cache stalls are intentionally outside the callback cadence contract.
// PA hold and checked duplex closure cover every flash write, including the
// boot confirmation marker. The existing input task supplies the later proof.
bool XiaomanBoxCodec::FirmwareAvailable() const {
    portENTER_CRITICAL(&mux_);
    const bool ok=playback_ && callbacks_ok_ && !setup_failed_ && !faulted_ && !firmware_paused_ && !firmware_restoring_;
    portEXIT_CRITICAL(&mux_);return ok;
}
bool XiaomanBoxCodec::FirmwareHealthy() {
    // Arm/Resume publish accepting_ only after lifecycle-owned readback;
    // every duplex change Disarms first. Sample that proof under mux_ so the
    // ordinary input task cannot starve the main tick's health observation.
    const auto now=esp_timer_get_time();
    portENTER_CRITICAL(&mux_);
    const bool ok=!firmware_paused_ && accepting_ && !faulted_ && playback_ && playback_->HardwareHealthy() &&
#if CONFIG_FAMILY_MUSIC_AUDITION
        !music_active_ &&
#endif
        firmware_dma_count_>=12 && last_dma_us_ && now-last_dma_us_<=11500 && input_success_us_ &&
        now-input_success_us_<=500000 && input_generation_==epochs_.capture();
    portEXIT_CRITICAL(&mux_);return ok;
}
bool XiaomanBoxCodec::PauseForFirmware() {
    std::unique_lock<std::mutex> lock(lifecycle_,std::defer_lock);const auto began=esp_timer_get_time();
    while(!lock.try_lock()){if(esp_timer_get_time()-began>=200000)return false;vTaskDelay(1);}
    if(firmware_paused_ || firmware_restoring_ || ReferenceFaulted() || PlaybackBusy())return false;
#if CONFIG_FAMILY_MUSIC_AUDITION
    if(music_active_)return false;
#endif
    portENTER_CRITICAL(&mux_);firmware_paused_=true;portEXIT_CRITICAL(&mux_);
    const bool muted=MuteMusicOutput(true,true);Disarm();
    if(!muted || !CloseDuplexForFirmware())return false;
    i2s_chan_info_t tx{},rx{};
    return i2s_channel_get_info(tx_handle_,&tx)==ESP_OK && i2s_channel_get_info(rx_handle_,&rx)==ESP_OK && !tx.is_enabled && !rx.is_enabled;
}
bool XiaomanBoxCodec::ResumeAfterFirmware() {
    std::unique_lock<std::mutex> lock(lifecycle_,std::defer_lock);const auto began=esp_timer_get_time();
    while(!lock.try_lock()){if(esp_timer_get_time()-began>=200000)return false;vTaskDelay(1);}
    if(!firmware_paused_)return !ReferenceFaulted();
    bool ok=!ReferenceFaulted() && MuteMusicOutput(true,true) && ReopenDuplexForMusic(24000,true) && VerifyHardware();
    if(ok) {
        portENTER_CRITICAL(&mux_);playback_->HardwareReset();probe_callbacks_=0;last_dma_us_=last_dma_gap_us_=0;recovery_probe_=true;portEXIT_CRITICAL(&mux_);
        const auto deadline=esp_timer_get_time()+500000;
        while(esp_timer_get_time()<deadline) {
            portENTER_CRITICAL(&mux_);const bool done=!recovery_probe_ || !playback_->HardwareHealthy() || probe_callbacks_>=12;portEXIT_CRITICAL(&mux_);
            if(done)break;
            vTaskDelay(1);
        }
        portENTER_CRITICAL(&mux_);
        ok=recovery_probe_ && playback_->HardwareHealthy() && probe_callbacks_>=12 && esp_timer_get_time()-last_dma_us_<=11500;
        recovery_probe_=false;
        if(ok){firmware_paused_=false;firmware_restoring_=true;accepting_=true;firmware_dma_count_=12;}
        portEXIT_CRITICAL(&mux_);
    }
    if(!ok){portENTER_CRITICAL(&mux_);FaultLocked(8,0,9);portEXIT_CRITICAL(&mux_);MuteMusicOutput(true,true);}
    return ok;
}
bool XiaomanBoxCodec::FinishFirmwareResume() {
    if(!FirmwareHealthy())return false;
    // The atomic restoration latch excludes duplex reconfiguration. InputData
    // may keep reading RX; output-register/PA work stays serialized by the
    // bounded data_if lock. No mutable I2S driver pointer is read here.
    if(!firmware_restoring_)return !ReferenceFaulted();
    bool ok=MuteMusicOutput(false,true);
    portENTER_CRITICAL(&mux_);
    ok=ok && !faulted_ && accepting_ && playback_->HardwareHealthy() && esp_timer_get_time()-last_dma_us_<=11500 &&
        input_success_us_ && esp_timer_get_time()-input_success_us_<=500000 && input_generation_==epochs_.capture();
    if(ok)firmware_restoring_=false;else FaultLocked(8,0,9);
    portEXIT_CRITICAL(&mux_);if(!ok)MuteMusicOutput(true,true);return ok;
}

// All Flash and codec driver operations run under lifecycle_, never in Sent().
void XiaomanBoxCodec::PersistFirstFault() {
    portENTER_CRITICAL(&mux_);
    const bool save=first_fault_.source && !fault_saved_;
    const auto snapshot=first_fault_;
    if(save)fault_saved_=true; // one attempt/boot, including unavailable/full NVS
    portEXIT_CRITICAL(&mux_);
    if(!save)return;
    MuteMusicOutput(true); // fault ISR already outputs zeros; do not reopen yet
    const bool stored=family_robot::SaveAudioFaultSnapshot(snapshot);
#ifdef ESP_PLATFORM
    ESP_LOGE("XiaomanBox", "first audio fault boot=%u source=%u detail=%u at_us=%lld bytes=%u gap_us=%lld native=%ld stored=%d",
        unsigned(snapshot.boot),unsigned(snapshot.source),unsigned(snapshot.detail),
        static_cast<long long>(snapshot.at_us),unsigned(snapshot.dma_bytes),
        static_cast<long long>(snapshot.dma_gap_us),static_cast<long>(snapshot.native),stored);
#else
    (void)stored;
#endif
}
void XiaomanBoxCodec::PollAudioFault() {
    // Called at the AudioService task entry, never from a lifecycle-owned query.
    portENTER_CRITICAL(&mux_);
    const bool pending=first_fault_.source && !fault_saved_;
    portEXIT_CRITICAL(&mux_);
    if(!pending)return;
    std::unique_lock<std::mutex> lock(lifecycle_,std::try_to_lock);
    if(lock.owns_lock())PersistFirstFault(); // busy: the next read entry retries
}
bool XiaomanBoxCodec::PrepareAudioConnection() {
    portENTER_CRITICAL(&mux_);const bool firmware=firmware_paused_ || firmware_restoring_;portEXIT_CRITICAL(&mux_);
    if(firmware)return false;
#if CONFIG_FAMILY_MUSIC_AUDITION
    if(MusicActive())return false;
#endif
    if(!ReferenceFaulted())return PlaybackSupported();
    // A blocked ordinary ADC read or lifecycle owner must not hang connection.
    std::unique_lock<std::mutex> lock(lifecycle_,std::defer_lock);
    const auto began=esp_timer_get_time();
    while(!lock.try_lock()) {
        if(esp_timer_get_time()-began>=200000)return false;
        vTaskDelay(1);
    }
    portENTER_CRITICAL(&mux_);
    const bool eligible=faulted_ && once_ready_ && callbacks_ok_ && playback_ && !setup_failed_ &&
        !capture_active_ && !recovery_attempted_ && output_generation_!=UINT32_MAX
#if CONFIG_FAMILY_MUSIC_AUDITION
        && !music_active_
#endif
        ;
    portEXIT_CRITICAL(&mux_);
    if(!eligible)return false; // busy/setup refusals do not spend the recovery
    PersistFirstFault(); // serialize NVS cache stalls BEFORE fresh callback evidence
    recovery_attempted_=true;
    const bool muted=MuteMusicOutput(true);
    Disarm(); // invalidates all old queued PCM, receipts, capture/AEC and wake epochs
    // Numeric phases: PA/mute, reopen, readback, DMA, input, freshness,
    // unmute/PA release, final publication. Exactly one task log per attempt.
    uint32_t phase=1;
    bool ok=muted;
    if(ok) {phase=2;ok=ReopenDuplexForMusic(24000);}
    if(ok) {phase=3;ok=VerifyHardware();}
    if(ok) {
        phase=4;
        portENTER_CRITICAL(&mux_);
        playback_->HardwareReset();probe_callbacks_=0;
        last_dma_us_=last_dma_gap_us_=0;last_dma_bytes_=0;
        recovery_probe_=true;
        portEXIT_CRITICAL(&mux_);
        const auto deadline=esp_timer_get_time()+500000;
        while(esp_timer_get_time()<deadline) {
            portENTER_CRITICAL(&mux_);
            const bool healthy=recovery_probe_ && playback_->HardwareHealthy();
            const bool ready=probe_callbacks_>=12;
            portEXIT_CRITICAL(&mux_);
            if(!healthy || ready)break;
            vTaskDelay(1);
        }
        portENTER_CRITICAL(&mux_);
        ok=recovery_probe_ && playback_->HardwareHealthy() && probe_callbacks_>=12;
        portEXIT_CRITICAL(&mux_);
    }
    int input_result=ESP_OK;
    if(ok) {
        phase=5;
        // Use the public timeout API: esp_codec_dev_read may block indefinitely.
        // This scratch contains no retained input and is never forwarded to AFE.
        int16_t scratch[480]{};size_t received=0;
        input_result=i2s_channel_read(rx_handle_,scratch,sizeof(scratch),&received,200);
        std::memset(scratch,0,sizeof(scratch));
        ok=input_result==ESP_OK && received==sizeof(scratch) && VerifyHardware();
    }
    if(ok)phase=6;
    // Input or a task stall may have invalidated the already-counted callbacks.
    // Check again BEFORE touching mute; an invalid probe never enables the DAC.
    portENTER_CRITICAL(&mux_);
    ok=ok && recovery_probe_ && playback_->HardwareHealthy() && probe_callbacks_>=12 &&
        esp_timer_get_time()-last_dma_us_<=11500 && !capture_active_;
    portEXIT_CRITICAL(&mux_);
    // Callbacks remain isolated during input verification and DAC unmute.
    if(ok) {phase=7;ok=MuteMusicOutput(false);}
    if(ok)phase=8;
    portENTER_CRITICAL(&mux_);
    ok=ok && recovery_probe_ && playback_->HardwareHealthy() && probe_callbacks_>=12 &&
        esp_timer_get_time()-last_dma_us_<=11500 && !capture_active_;
    recovery_probe_=false;
    if(ok) {faulted_=false;accepting_=true;aec_ready_=UINT32_MAX;}
    else FaultLocked(8,input_result);
    portEXIT_CRITICAL(&mux_);
    if(!ok)MuteMusicOutput(true);
#ifdef ESP_PLATFORM
    ESP_LOGI("XiaomanBox", "audio recovery attempt=1 result=%d phase=%u dma=%u input=%d pa_error=%d",
        ok,unsigned(phase),unsigned(probe_callbacks_),input_result,OutputInhibitError());
#else
    (void)phase;
#endif
    return ok;
}

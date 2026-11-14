// Music owns the same bounded output/DMA queue, without a voice receipt identity.
// Caller has suspended AudioService producers before changing the shared clocks.
bool XiaomanBoxCodec::MusicActive() const {
    portENTER_CRITICAL(&mux_);const bool active=music_active_;portEXIT_CRITICAL(&mux_);return active;
}
bool XiaomanBoxCodec::BeginMusic(int rate) {
    std::lock_guard<std::mutex> lock(lifecycle_);
    if(firmware_paused_ || firmware_restoring_)return false;
    if((rate!=24000 && rate!=48000) || music_active_ || ReferenceFaulted() || PlaybackBusy())return false;
    portENTER_CRITICAL(&mux_);music_active_=true;music_rate_=rate;portEXIT_CRITICAL(&mux_);
    const bool muted=MuteMusicOutput(true);
    Disarm();
    if(!muted || !ReopenDuplexForMusic(rate)) {
        portENTER_CRITICAL(&mux_);FaultLocked(6);portEXIT_CRITICAL(&mux_);return false;
    }
    portENTER_CRITICAL(&mux_);playback_->HardwareReset(rate);portEXIT_CRITICAL(&mux_);
    Arm();
#ifdef ESP_PLATFORM
    // The DAC stays muted for a full descriptor rotation. Old physical DMA
    // bytes cannot sound after a clock/profile change, even with empty queues.
    vTaskDelay(pdMS_TO_TICKS(80));
#endif
    if(ReferenceFaulted() || !MuteMusicOutput(false)) {
        portENTER_CRITICAL(&mux_);FaultLocked(6);portEXIT_CRITICAL(&mux_);return false;
    }
    return true;
}
bool XiaomanBoxCodec::EndMusic() {
    std::lock_guard<std::mutex> lock(lifecycle_);
    if(!music_active_)return output_sample_rate_==24000 && !ReferenceFaulted();
    const bool muted=MuteMusicOutput(true);
    Disarm();
    portENTER_CRITICAL(&mux_);music_rate_=24000;portEXIT_CRITICAL(&mux_);
    if(!muted || !ReopenDuplexForMusic(24000)) {
        portENTER_CRITICAL(&mux_);FaultLocked(7);portEXIT_CRITICAL(&mux_);return false;
    }
    portENTER_CRITICAL(&mux_);playback_->HardwareReset();portEXIT_CRITICAL(&mux_);
    Arm();
#ifdef ESP_PLATFORM
    vTaskDelay(pdMS_TO_TICKS(80));
#endif
    if(ReferenceFaulted() || !MuteMusicOutput(false))return false;
    portENTER_CRITICAL(&mux_);music_active_=false;portEXIT_CRITICAL(&mux_);
    return VerifyHardware();
}
bool XiaomanBoxCodec::MusicWrite(std::vector<int16_t>& pcm) {
    if(!MusicActive() || pcm.empty() || pcm.size()>1440 || ReferenceFaulted())return false;
    const auto generation=PlaybackGeneration();
    return SubmitOutput(pcm,{},0,generation) && MusicActive() &&
           !ReferenceFaulted() && generation==PlaybackGeneration();
}
uint64_t XiaomanBoxCodec::MusicCompleted() const {
    portENTER_CRITICAL(&mux_);const auto samples=playback_?playback_->UntaggedCompleted():0;portEXIT_CRITICAL(&mux_);return samples;
}

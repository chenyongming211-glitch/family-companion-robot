// Included by the production codec and host adapter harness. No alternate implementation.
bool SoftwareAecCodec::PlaybackControl(PlaybackOp op, family_robot::PlaybackIdentity id, uint64_t first, size_t n) {
    portENTER_CRITICAL(&mux_);
    bool result = playback_ != nullptr;
    if (playback_) {
        switch (op) {
        case PlaybackOp::Connect: playback_->Connect(id.connection); break;
        case PlaybackOp::Start: result = playback_->Start(id); break;
        case PlaybackOp::Admit: result = playback_->Admit(id, first, n); break;
        case PlaybackOp::Release: playback_->Release(id); break;
        case PlaybackOp::Seal: playback_->Seal(id, false); break;
        case PlaybackOp::Interrupt: playback_->Seal(id, true); break;
        case PlaybackOp::Invalidate: playback_->Invalidate(id); break;
        case PlaybackOp::Reset: playback_->InterruptAll();
            if (output_generation_ != UINT32_MAX) ++output_generation_;
            else playback_->HardwareFault();
            break;
        }
    }
    portEXIT_CRITICAL(&mux_);
    return result;
}
uint32_t SoftwareAecCodec::PlaybackGeneration() const {
    portENTER_CRITICAL(&mux_);
    const auto value = output_generation_;
    portEXIT_CRITICAL(&mux_);
    return value;
}
bool SoftwareAecCodec::PlaybackPoll(family_robot::PlaybackSnapshot& out) {
    portENTER_CRITICAL(&mux_);
    const bool result = playback_ && playback_->NextReport(esp_timer_get_time(), out);
    portEXIT_CRITICAL(&mux_);
    return result;
}
bool SoftwareAecCodec::PlaybackBusy() const {
    portENTER_CRITICAL(&mux_);
    const bool result = playback_ && playback_->Busy();
    portEXIT_CRITICAL(&mux_);
    return result;
}
void SoftwareAecCodec::OutputData(std::vector<int16_t>& data) {
    OutputTagged(data, {}, 0, PlaybackGeneration());
}
void SoftwareAecCodec::OutputTagged(std::vector<int16_t>& data, family_robot::PlaybackIdentity id,
                                   uint64_t first, uint32_t generation) {
    // Single task producer. lifecycle_ prevents physical close/reopen during a
    // submission; mux_ serializes each small copy against cancellation and ISR.
    // Never invoke esp_codec_dev_write/i2s_channel_write in callback-refill mode.
    std::lock_guard<std::mutex> lock(lifecycle_);
    size_t offset = 0;
    int64_t last_progress = esp_timer_get_time();
    while (offset < data.size()) {
        const size_t n = std::min(data.size()-offset, family_robot::PlaybackProgress::kBlock);
        portENTER_CRITICAL(&mux_);
        const bool usable = playback_ && accepting_ && output_enabled_ && output_generation_ != UINT32_MAX && generation == output_generation_;
        const auto result = usable ? playback_->Submit(id, first+offset, data.data()+offset, n)
            : family_robot::PlaybackProgress::SubmitResult::Rejected;
        if (!usable && playback_ && generation == output_generation_) playback_->Invalidate(id);
        portEXIT_CRITICAL(&mux_);
        if (result == family_robot::PlaybackProgress::SubmitResult::Rejected) return;
        if (result == family_robot::PlaybackProgress::SubmitResult::Accepted) {
            offset += n; last_progress = esp_timer_get_time(); continue;
        }
        if (esp_timer_get_time()-last_progress > 200000) {
            PlaybackControl(PlaybackOp::Invalidate, id);
            return;
        }
        vTaskDelay(1); // backpressure outside critical section; reset never waits
    }
}

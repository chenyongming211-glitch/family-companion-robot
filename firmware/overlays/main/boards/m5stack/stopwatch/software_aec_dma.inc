// Actual ISR callback body; compiled by the production codec and native harness.
bool SoftwareAecCodec::Complete(i2s_event_data_t* e, bool tx) {
    const int64_t stamp = esp_timer_get_time();
    portENTER_CRITICAL_ISR(&mux_);
    if (accepting_ && pair_) {
        auto& discard = tx ? discard_tx_ : discard_rx_;
        if (e->size != D::kBlock * sizeof(int16_t) || !e->dma_buf)
            pair_->Fail(D::Fault::Format);
        else if (discard)
            --discard;
        else if (tx) {
            pair_->Tx(pair_->tx_samples(), stamp, static_cast<int16_t*>(e->dma_buf), D::kBlock);
            last_tx_us_ = stamp;
        } else {
            pair_->Rx(pair_->rx_samples(), stamp, static_cast<int16_t*>(e->dma_buf), D::kBlock);
            last_rx_us_ = stamp;
        }
        PublishLocked();
    }
    if (tx && e->dma_buf) {
        // pair_->Tx above observes the actual old physical bytes. Refill only
        // afterwards; DMA address and tag remain owned until its next callback.
        if (playback_ && accepting_ && e->size == D::kBlock * sizeof(int16_t)) {
            playback_->Complete(static_cast<int16_t*>(e->dma_buf), D::kBlock, stamp);
        } else {
            if (playback_) playback_->HardwareFault();
            memset(e->dma_buf, 0, e->size);
        }
    }
    portEXIT_CRITICAL_ISR(&mux_);
    return false;
}

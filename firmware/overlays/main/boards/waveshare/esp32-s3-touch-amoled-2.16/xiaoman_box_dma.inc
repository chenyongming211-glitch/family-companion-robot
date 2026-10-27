bool XiaomanBoxCodec::Sent(i2s_chan_handle_t,i2s_event_data_t* event,void* arg) {return static_cast<XiaomanBoxCodec*>(arg)->Complete(event);}
bool XiaomanBoxCodec::Complete(i2s_event_data_t* event) {
    const auto now=esp_timer_get_time();
    portENTER_CRITICAL_ISR(&mux_);
    if(accepting_) {
        last_dma_bytes_=event?event->size:0;
        last_dma_gap_us_=last_dma_us_?now-last_dma_us_:0;
        last_dma_us_=now;
    }
    if(accepting_ && (!event || !event->dma_buf || event->size!=480)) FaultLocked(2);
    else if(accepting_ && playback_) {
        // The old tag is credited only when its descriptor physically completes;
        // PlaybackProgress then fills that exact descriptor with the next PCM.
        playback_->Complete(static_cast<int16_t*>(event->dma_buf),240,now);
        if(!playback_->HardwareHealthy())FaultLocked(3);
    }
    if(event && event->dma_buf && !accepting_)std::memset(event->dma_buf,0,event->size);
    portEXIT_CRITICAL_ISR(&mux_);return false;
}

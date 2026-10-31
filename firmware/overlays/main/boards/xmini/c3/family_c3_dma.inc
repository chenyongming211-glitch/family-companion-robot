bool FamilyC3Codec::Sent(i2s_chan_handle_t,i2s_event_data_t* event,void* context) {
    return static_cast<FamilyC3Codec*>(context)->Complete(event);
}
bool FamilyC3Codec::Complete(i2s_event_data_t* event) {
    const auto now=esp_timer_get_time();
    portENTER_CRITICAL_ISR(&mux_);
    const bool shape=event && event->dma_buf && event->size==480;
    if((accepting_ || scrub_pending_) && !shape)FaultLocked();
    if(shape) {
        auto* address=static_cast<int16_t*>(event->dma_buf);
        bool known=false;
        for(size_t i=0;i<known_descriptors_;++i)if(dma_addresses_[i]==address)known=true;
        if(!known && hardware_open_) {
            if(known_descriptors_==family_robot::PlaybackProgress::kSlots)FaultLocked();
            else dma_addresses_[known_descriptors_++]=address;
        }
        if(accepting_ && playback_) {
            // Credit only the old tag on an actually completed descriptor;
            // then refill this same descriptor with the next bounded PCM block.
            playback_->Complete(address,family_robot::PlaybackProgress::kBlock,now);
            last_tx_us_=now;
            if(!playback_->Busy())pending_since_us_=0;
            if(!playback_->HardwareHealthy())FaultLocked();
        }
        if(!accepting_) {
            std::memset(address,0,480);
            if(scrub_pending_ && !faulted_) {
                bool repeated=false;
                for(size_t i=0;i<scrubbed_;++i)if(scrub_addresses_[i]==address)repeated=true;
                if(repeated)FaultLocked();
                else {
                    scrub_addresses_[scrubbed_++]=address;
                    if(scrubbed_==family_robot::PlaybackProgress::kSlots)scrub_pending_=false;
                }
            }
        }
    } else if(event && event->dma_buf) {
        // Malformed size is untrusted. Never let an error callback turn an
        // alleged size into an unbounded ISR memset or overrun a shorter buffer.
        std::memset(event->dma_buf,0,std::min(event->size,size_t(480)));
    }
    portEXIT_CRITICAL_ISR(&mux_);return false;
}

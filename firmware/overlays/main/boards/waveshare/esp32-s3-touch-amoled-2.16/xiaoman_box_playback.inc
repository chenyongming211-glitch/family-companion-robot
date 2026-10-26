bool XiaomanBoxCodec::PlaybackControl(PlaybackOp op,family_robot::PlaybackIdentity id,uint64_t first,size_t n) {
    // OutputTagged holds lifecycle_ while waiting for ISR space. Reset/Connect
    // must remain able to cancel it; only Start needs the physical lifecycle.
    std::unique_lock<std::mutex> lock(lifecycle_,std::defer_lock);
    if(op==PlaybackOp::Start) {lock.lock();if(!PreparePlaybackHardware())return false;}
    portENTER_CRITICAL(&mux_);
    bool result=playback_!=nullptr;
    if(result) switch(op) {
        case PlaybackOp::Connect:
            // A new transport revokes processed capture readiness. The wake
            // invocation froze its legitimate batch before opening this socket;
            // preserve that wake generation, as intentional capture pause does.
            playback_->Connect(id.connection);epochs_.Boundary();aec_ready_=UINT32_MAX;
            if(output_generation_!=UINT32_MAX)++output_generation_;
            else FaultLocked();
            break;
        case PlaybackOp::Start:result=accepting_ && !faulted_ && playback_->Start(id);break;
        case PlaybackOp::Admit:result=playback_->Admit(id,first,n);break;
        case PlaybackOp::Release:playback_->Release(id);break;
        case PlaybackOp::Seal:playback_->Seal(id,false);break;
        case PlaybackOp::Interrupt:playback_->Seal(id,true);break;
        case PlaybackOp::Invalidate:playback_->Invalidate(id);break;
        case PlaybackOp::Reset:
            if(output_generation_!=UINT32_MAX)++output_generation_;else FaultLocked();
            playback_->InterruptAll();break;
    }
    portEXIT_CRITICAL(&mux_);return result;
}
uint32_t XiaomanBoxCodec::PlaybackGeneration() const {portENTER_CRITICAL(&mux_);const auto n=output_generation_;portEXIT_CRITICAL(&mux_);return n;}
bool XiaomanBoxCodec::PlaybackPoll(family_robot::PlaybackSnapshot& out) {
    portENTER_CRITICAL(&mux_);const bool ok=playback_ && playback_->NextReport(esp_timer_get_time(),out);portEXIT_CRITICAL(&mux_);return ok;
}
bool XiaomanBoxCodec::PlaybackBusy() const {
    portENTER_CRITICAL(&mux_);const bool busy=playback_ && playback_->Busy();portEXIT_CRITICAL(&mux_);return busy;
}
#if CONFIG_XIAOMAN_ORB_UI
family_robot::OrbOutputActivity XiaomanBoxCodec::GetOrbOutputActivity() const {
    portENTER_CRITICAL(&mux_);const auto out=playback_?playback_->OrbActivity():family_robot::OrbOutputActivity{};portEXIT_CRITICAL(&mux_);return out;
}
void XiaomanBoxCodec::MarkOrbDraining(family_robot::PlaybackIdentity id) {portENTER_CRITICAL(&mux_);if(playback_)playback_->MarkOrbDraining(id);portEXIT_CRITICAL(&mux_);}
#endif
void XiaomanBoxCodec::OutputData(std::vector<int16_t>& data) {OutputTagged(data,{},0,PlaybackGeneration());}
void XiaomanBoxCodec::OutputTagged(std::vector<int16_t>& pcm,family_robot::PlaybackIdentity id,uint64_t first,uint32_t generation) {
    std::lock_guard<std::mutex> lock(lifecycle_);
    // Never call esp_codec_dev_write/i2s_channel_write, including failed setup.
    size_t offset=0;int64_t last_progress=esp_timer_get_time();
    while(offset<pcm.size()) {
        const size_t n=std::min(pcm.size()-offset,family_robot::PlaybackProgress::kBlock);
#if CONFIG_XIAOMAN_ORB_UI
        const auto level=family_robot::OrbPcmLevel(pcm.data()+offset,n);
#endif
        portENTER_CRITICAL(&mux_);
        const bool current=playback_ && accepting_ && !faulted_ && generation!=UINT32_MAX && generation==output_generation_;
        const auto result=current?playback_->Submit(id,first+offset,pcm.data()+offset,n
#if CONFIG_XIAOMAN_ORB_UI
            ,level
#endif
            ):family_robot::PlaybackProgress::SubmitResult::Rejected;
        if(!current && playback_ && generation==output_generation_)playback_->Invalidate(id);
        portEXIT_CRITICAL(&mux_);
        if(result==family_robot::PlaybackProgress::SubmitResult::Rejected)return;
        if(result==family_robot::PlaybackProgress::SubmitResult::Accepted) {offset+=n;last_progress=esp_timer_get_time();continue;}
        if(esp_timer_get_time()-last_progress>200000) {PlaybackControl(PlaybackOp::Invalidate,id);return;}
        vTaskDelay(1);
    }
}

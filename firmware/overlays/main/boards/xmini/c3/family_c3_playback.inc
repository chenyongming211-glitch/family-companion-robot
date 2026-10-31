bool FamilyC3Codec::PlaybackControl(PlaybackOp op,family_robot::PlaybackIdentity id,uint64_t first,size_t n) {
    // Connect/Reset must cancel a producer that is waiting under lifecycle_.
    std::unique_lock<std::mutex> lock(lifecycle_,std::defer_lock);
    if(op==PlaybackOp::Start) {lock.lock();if(!PrepareHardware())return false;}
    portENTER_CRITICAL(&mux_);
    bool result=playback_!=nullptr;
    if(result)switch(op) {
        case PlaybackOp::Connect:
            // Never let old descriptor identities alias a fresh connection.
            // The owning WebsocketProtocol already allocates monotonic epochs.
            if(!id.connection || id.connection<=last_connection_) {result=false;break;}
            if(id.connection==UINT32_MAX) {FaultLocked();result=false;break;}
            last_connection_=id.connection;AdvanceGenerationLocked();playback_->Connect(id.connection);break;
        case PlaybackOp::Start:result=accepting_ && !faulted_ && playback_->Start(id);break;
        case PlaybackOp::Admit:result=accepting_ && !faulted_ && playback_->Admit(id,first,n);break;
        case PlaybackOp::Release:playback_->Release(id);break;
        case PlaybackOp::Seal:playback_->Seal(id,false);break;
        case PlaybackOp::Interrupt:playback_->Seal(id,true);break;
        case PlaybackOp::Invalidate:playback_->Invalidate(id);break;
        case PlaybackOp::Reset:AdvanceGenerationLocked();playback_->InterruptAll();break;
    }
    portEXIT_CRITICAL(&mux_);return result;
}
uint32_t FamilyC3Codec::PlaybackGeneration() const {
    portENTER_CRITICAL(&mux_);const auto generation=output_generation_;portEXIT_CRITICAL(&mux_);return generation;
}
bool FamilyC3Codec::PlaybackPoll(family_robot::PlaybackSnapshot& out) {
    portENTER_CRITICAL(&mux_);
    // A callback can validate its spacing only if another callback arrives.
    // Short sealed replies need this task-side deadline too. Arm from the
    // first pending PCM, rather than stale idle timestamps, and suspend when
    // nothing remains in the queue/DMA ring or hardware is intentionally off.
    if(accepting_ && playback_ && playback_->Busy()) {
        const auto latest=std::max(pending_since_us_,last_tx_us_);
        const auto now=esp_timer_get_time();
        if(latest<=0 || now<latest || now-latest>200000)FaultLocked();
    }
    const bool fault=faulted_;
    portEXIT_CRITICAL(&mux_);
    if(fault)StopFaultedOutput();
    portENTER_CRITICAL(&mux_);
    const bool report=playback_ && playback_->NextReport(esp_timer_get_time(),out);
    portEXIT_CRITICAL(&mux_);return report;
}
bool FamilyC3Codec::PlaybackBusy() const {
    portENTER_CRITICAL(&mux_);const bool busy=playback_ && playback_->Busy();portEXIT_CRITICAL(&mux_);return busy;
}
void FamilyC3Codec::OutputData(std::vector<int16_t>& data) {OutputTagged(data,{},0,PlaybackGeneration());}
void FamilyC3Codec::OutputTagged(std::vector<int16_t>& pcm,family_robot::PlaybackIdentity id,uint64_t first,uint32_t generation) {
    std::lock_guard<std::mutex> lock(lifecycle_);
    size_t offset=0;int64_t progress=esp_timer_get_time();
    while(offset<pcm.size()) {
        const size_t n=std::min(pcm.size()-offset,family_robot::PlaybackProgress::kBlock);
        portENTER_CRITICAL(&mux_);
        const bool current=playback_ && accepting_ && !faulted_ && generation!=UINT32_MAX && generation==output_generation_;
        const bool range=first<=family_robot::PlaybackProgress::kMaxSamples &&
            offset<=family_robot::PlaybackProgress::kMaxSamples-first;
        const bool was_busy=playback_ && playback_->Busy();
        const auto result=current && range ? playback_->Submit(id,first+offset,pcm.data()+offset,n)
            :family_robot::PlaybackProgress::SubmitResult::Rejected;
        if(result==family_robot::PlaybackProgress::SubmitResult::Accepted && !was_busy)
            pending_since_us_=esp_timer_get_time();
        if(result==family_robot::PlaybackProgress::SubmitResult::Rejected && playback_ && generation==output_generation_)
            playback_->Invalidate(id);
        portEXIT_CRITICAL(&mux_);
        if(result==family_robot::PlaybackProgress::SubmitResult::Rejected)return;
        if(result==family_robot::PlaybackProgress::SubmitResult::Accepted) {offset+=n;progress=esp_timer_get_time();continue;}
        if(esp_timer_get_time()-progress>200000) {
            portENTER_CRITICAL(&mux_);FaultLocked();portEXIT_CRITICAL(&mux_);return;
        }
        vTaskDelay(1); // no mux held: ISR progress, Connect and Reset remain live
    }
}

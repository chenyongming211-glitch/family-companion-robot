// Compiled by the production codec and the cloud fault harness.
void FamilyC3Codec::ResetCaptureLocked() {
    reference_valid_=false;aec_ready_generation_=UINT32_MAX;
    if(capture_generation_>=UINT32_MAX-1) {
        capture_generation_=UINT32_MAX;faulted_=true;accepting_=false;
        reference_callbacks_armed_=false;
        if(playback_)playback_->HardwareFault();
        if(pair_)pair_->Fail(Pair::Fault::Sequence);
    } else ++capture_generation_;
    capture_boundary_pending_=capture_active_ && !faulted_;
    if(pair_)pair_->Capture(false); // preserve physical TX history and clocks
}
void FamilyC3Codec::CheckReferenceDeadlineLocked(int64_t now) {
    if(capture_active_ && reference_callbacks_armed_ && hardware_open_ &&
       (now-reference_tx_us_>Pair::kHoldbackUs || now-reference_rx_us_>Pair::kHoldbackUs ||
        now+Pair::kCadenceToleranceUs<reference_tx_us_ || now+Pair::kCadenceToleranceUs<reference_rx_us_)) {
        if(pair_)pair_->Fail(Pair::Fault::Timeout);
        FaultLocked();
    }
}
void FamilyC3Codec::PublishReferenceLocked() {
    if(!pair_ || pair_->fault()!=Pair::Fault::None) {FaultLocked();return;}
    reference_valid_=capture_active_ && !capture_boundary_pending_ && pair_->eligible() && !faulted_;
}
void FamilyC3Codec::SetCaptureActive(bool active) {
    portENTER_CRITICAL(&mux_);
    if(capture_active_!=active) {
        capture_active_=active && !faulted_;
        ResetCaptureLocked();
    }
    portEXIT_CRITICAL(&mux_);
}
uint32_t FamilyC3Codec::CaptureGeneration() const {
    portENTER_CRITICAL(&mux_);const auto generation=capture_generation_;portEXIT_CRITICAL(&mux_);return generation;
}
bool FamilyC3Codec::ReferenceValid() const {
    portENTER_CRITICAL(&mux_);const bool valid=reference_valid_ && !faulted_;portEXIT_CRITICAL(&mux_);return valid;
}
bool FamilyC3Codec::RealtimeEligible() const {
    portENTER_CRITICAL(&mux_);
    const bool ready=reference_valid_ && !faulted_ && capture_active_ && aec_ready_generation_==capture_generation_;
    portEXIT_CRITICAL(&mux_);return ready;
}
void FamilyC3Codec::NotifyAecReady(bool ready,uint32_t generation) {
    portENTER_CRITICAL(&mux_);
    aec_ready_generation_=ready && reference_valid_ && !faulted_ && generation==capture_generation_ ? generation : UINT32_MAX;
    portEXIT_CRITICAL(&mux_);
}
void FamilyC3Codec::ReportReferenceFailure() {
    portENTER_CRITICAL(&mux_);FaultLocked();portEXIT_CRITICAL(&mux_);StopFaultedOutput();
}
bool FamilyC3Codec::InputPaired(std::vector<int16_t>& data) {
    constexpr size_t kMaxFrames=1440;
    if(data.empty() || data.size()%2 || data.size()/2>kMaxFrames) {
        ReportReferenceFailure();return false;
    }
    const auto generation=CaptureGeneration();
    const size_t frames=data.size()/2;
    const int64_t deadline=esp_timer_get_time()+Pair::kHoldbackUs+static_cast<int64_t>(frames)*1000000/Pair::kRate;
    size_t done=0;
    for(;;) {
        portENTER_CRITICAL(&mux_);
        CheckReferenceDeadlineLocked(esp_timer_get_time());
        if(faulted_ || !capture_active_ || generation!=capture_generation_ || !hardware_open_ || !input_enabled_) {
            const bool fault=faulted_;portEXIT_CRITICAL(&mux_);
            if(fault)StopFaultedOutput();
            return false;
        }
        const size_t count=pair_ ? std::min(frames-done,pair_->front_remaining()) : 0;
        const auto result=pair_ ? pair_->Take(data.data()+done*2,count,esp_timer_get_time()) : Pair::Result::Fault;
        PublishReferenceLocked();
        const bool fault=faulted_;
        portEXIT_CRITICAL(&mux_);
        if(fault || result==Pair::Result::Fault) {StopFaultedOutput();return false;}
        if(result==Pair::Result::Ready) {
            done+=count;
            if(done==frames) {
                portENTER_CRITICAL(&mux_);
                const bool current=!faulted_ && capture_active_ && generation==capture_generation_;
                portEXIT_CRITICAL(&mux_);return current;
            }
        }
        // Warmup discarded a pre-epoch RX block. Continue to fresh, matched MR;
        // no raw-microphone fallback or partially paired result may be returned.
        if(esp_timer_get_time()>=deadline) {ReportReferenceFailure();return false;}
        if(result!=Pair::Result::Ready && result!=Pair::Result::Warmup)vTaskDelay(1);
    }
}

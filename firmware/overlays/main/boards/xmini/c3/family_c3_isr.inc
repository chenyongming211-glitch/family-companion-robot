// ISR definitions shared byte-for-byte with the legacy host/adapter path.
#if CONFIG_XMINI_C3_PCM
int64_t FamilyC3Codec::DiagnosticNow() {
    return esp_timer_get_time(); // IDF timer read is ISR-safe; no heap/log/locks
}
void FamilyC3Codec::RecordStartupFaultLocked(StartupFaultSource source,int64_t now,int64_t previous_us) {
    if(startup_fault_.source!=StartupFaultSource::None)return;
    startup_fault_.timestamp_us=now;startup_fault_.hardware_open=hardware_open_;
    startup_fault_.callback_previous_us=previous_us;
    auto& check=startup_fault_.hardware;
    check.mask=hardware_check_.mask;check.channel=hardware_check_.channel;
    check.tx_result=hardware_check_.tx_result;check.rx_result=hardware_check_.rx_result;check.state=hardware_check_.state;
    for(unsigned i=0;i<14;++i)check.actual[i]=hardware_check_.actual[i];
#if CONFIG_XMINI_C3_VOICE_BARGE
    startup_fault_.pair_fault=pair_?static_cast<uint32_t>(pair_->fault()):UINT32_MAX;
    startup_fault_.capture_active=capture_active_;startup_fault_.callbacks_armed=reference_callbacks_armed_;
    startup_fault_.tx_us=reference_tx_us_;startup_fault_.rx_us=reference_rx_us_;
    startup_fault_.tx_samples=pair_?pair_->tx_samples():0;startup_fault_.rx_samples=pair_?pair_->rx_samples():0;
#endif
    startup_fault_.source=source; // last store; every reader also owns mux_
}
#endif
#if CONFIG_XMINI_C3_PCM
void FamilyC3Codec::FaultLocked(StartupFaultSource source,int64_t now,int64_t previous_us)
#else
void FamilyC3Codec::FaultLocked()
#endif
{
#if CONFIG_XMINI_C3_PCM
    RecordStartupFaultLocked(source,now,previous_us);
#endif
#if CONFIG_XMINI_C3_VOICE_BARGE
    if(!faulted_) {capture_active_=false;ResetCaptureLocked();}
    reference_callbacks_armed_=false;reference_valid_=false;aec_ready_generation_=UINT32_MAX;
    if(pair_)pair_->Fail(Pair::Fault::Setup);
#endif
    faulted_=true;accepting_=false;
    if(playback_)playback_->HardwareFault();
}

#if CONFIG_XMINI_C3_VOICE_BARGE
void FamilyC3Codec::ResetCaptureLocked() {
    reference_valid_=false;aec_ready_generation_=UINT32_MAX;
    if(capture_generation_>=UINT32_MAX-1) {
#if CONFIG_XMINI_C3_PCM
        RecordStartupFaultLocked(StartupFaultSource::CaptureGeneration,DiagnosticNow());
#endif
        capture_generation_=UINT32_MAX;faulted_=true;accepting_=false;
        reference_callbacks_armed_=false;
        if(playback_)playback_->HardwareFault();
        if(pair_)pair_->Fail(Pair::Fault::Sequence);
    } else ++capture_generation_;
    capture_boundary_pending_=capture_active_ && !faulted_;
    if(pair_)pair_->Capture(false); // preserve physical TX history and clocks
}
#if CONFIG_XMINI_C3_PCM
void FamilyC3Codec::PublishReferenceLocked(StartupFaultSource source,int64_t now,int64_t previous_us)
#else
void FamilyC3Codec::PublishReferenceLocked()
#endif
{
    if(!pair_ || pair_->fault()!=Pair::Fault::None) {FaultLocked(FAMILY_C3_DIAGNOSTIC_ARGS(source,now,previous_us));return;}
    reference_valid_=capture_active_ && !capture_boundary_pending_ && pair_->eligible() && !faulted_;
}

#endif
#include "family_c3_dma.inc"

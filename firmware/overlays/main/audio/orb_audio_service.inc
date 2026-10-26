// Task-only, existing audio queue mutex; no display calls and no new audio locks.
family_robot::OrbAudioActivity AudioService::GetOrbAudioActivity() {
    family_robot::OrbAudioActivity out{};
    if(!codec_ || !codec_->ManagedCapture()) return out;
    out.capture=codec_->CaptureGeneration();
    out.epoch=orb_gate_epoch_.load();
    out.output=codec_->GetOrbOutputActivity();
    // Hardware MR must have emitted a current processed AFE frame. M5 keeps
    // its established software-reference observation behavior.
    if(!codec_->SoftwareReference() && !codec_->RealtimeEligible()) return out;
    // These safety facts do not require the queue mutex. A pause/fault changes
    // capture, a gate transition changes the UI-only published epoch, and a
    // reconnect changes output.connection even while the queue is contended.
    if(!IsAudioProcessorRunning() || capture_generation_.load()!=out.capture) return out;
    std::unique_lock<std::mutex> lock(audio_queue_mutex_,std::try_to_lock);
    if(!lock) {out.snapshot_available=false;return out;}
    out.capture=codec_->CaptureGeneration();
    out.epoch=experiment_gate_.epoch();
    out.gate_open=IsAudioProcessorRunning() &&
        experiment_gate_.Allows(out.epoch) && capture_generation_.load()==out.capture &&
        experiment_authorization_.Allows(codec_->ReferenceFaulted(),codec_->ReferenceIntegrity());
    out.input=orb_input_activity_;
    return out;
}

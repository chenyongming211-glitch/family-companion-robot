// Task-only, existing audio queue mutex; no display calls and no new audio locks.
family_robot::OrbAudioActivity AudioService::GetOrbAudioActivity() {
    family_robot::OrbAudioActivity out{};
    std::unique_lock<std::mutex> lock(audio_queue_mutex_,std::try_to_lock);
    if(!lock || !codec_ || !codec_->SoftwareReference()) return out;
    out.capture=codec_->CaptureGeneration();
    out.epoch=experiment_gate_.epoch();
    out.gate_open=IsAudioProcessorRunning() &&
        experiment_gate_.Allows(out.epoch) && capture_generation_.load()==out.capture &&
        experiment_authorization_.Allows(codec_->ReferenceFaulted(),codec_->ReferenceIntegrity());
    out.input=orb_input_activity_;
    out.output=codec_->GetOrbOutputActivity();
    return out;
}

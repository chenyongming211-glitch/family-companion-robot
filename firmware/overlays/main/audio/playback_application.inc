#define FAMILY_ROBOT_DRAIN_APPLICATION 1
// Actual scheduled application transitions, also compiled by the native harness.
void Application::ScheduleAudioChannelClosed(uint32_t epoch) {
    Schedule([this, epoch] {
        if (epoch && (!protocol_ || protocol_->AudioChannelEpoch() != epoch)) return;
        auto& board = Board::GetInstance();
        board.SetPowerSaveLevel(PowerSaveLevel::LOW_POWER);
        board.GetDisplay()->SetChatMessage("system", "");
        SetDeviceState(kDeviceStateIdle);
    });
}
void Application::SchedulePlaybackStart(family_robot::PlaybackEvent event) {
    Schedule([this, event] {
        if (event && (!protocol_ || !protocol_->PlaybackEventCurrent(event))) return;
        aborted_ = false;
        SetDeviceState(kDeviceStateSpeaking);
    });
}
void Application::SchedulePlaybackStop(family_robot::PlaybackEvent event, bool interrupted) {
    Schedule([this, event, interrupted] {
        if (event && (!protocol_ || !protocol_->PlaybackEventCurrent(event))) return;
        if (interrupted && !event) audio_service_.ResetDecoder();
        if (GetDeviceState() == kDeviceStateSpeaking) {
            SetDeviceState(listening_mode_ == kListeningModeManualStop
                ? kDeviceStateIdle : kDeviceStateListening);
        }
    });
}

// Barge-in in drain mode cancels production without revoking received PCM.
void Application::AbortSpeaking(AbortReason reason) {
    ESP_LOGI(TAG, "Abort speaking");
    aborted_ = true;
    if (protocol_ && protocol_->PlaybackNegotiated() && !protocol_->PlaybackDrainNegotiated())
        audio_service_.ResetDecoder();
    if (protocol_) protocol_->SendAbortSpeaking(reason);
}

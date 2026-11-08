#define FAMILY_ROBOT_DRAIN_APPLICATION 1
// Actual scheduled application transitions, also compiled by the native harness.
void Application::ScheduleAudioChannelClosed(uint32_t epoch) {
    Schedule([this, epoch] {
        if (epoch && (!protocol_ || protocol_->AudioChannelEpoch() != epoch)) return;
        auto& board = Board::GetInstance();
        board.SetPowerSaveLevel(PowerSaveLevel::LOW_POWER);
#if CONFIG_CHICK_FACE_UI
        board.GetDisplay()->ClearPlaybackEmotions();
#endif
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

#if CONFIG_CHICK_FACE_UI
void Application::ScheduleLegacyEmotion(uint32_t connection,std::string emotion) {
    Schedule([this,connection,emotion=std::move(emotion)] {
        if(!protocol_ || !protocol_->LegacyEmotionCurrent(connection))return;
        Board::GetInstance().GetDisplay()->SetLegacyEmotion(connection,emotion.c_str());
    });
}
void Application::SchedulePlaybackExpression(family_robot::PlaybackExpressionEvent event) {
    Schedule([this,event] {
        // A normal tts.stop seals ingress but does not end physical playback.
        // Cancel/drain/new start/close revoke the protocol lease before main.
        if(!protocol_ || !protocol_->PlaybackExpressionCurrent(event))return;
        Board::GetInstance().GetDisplay()->SetPlaybackEmotion(event.identity.connection,
            event.identity.playback,event.EmotionName(),event.arrival_us);
    });
}
#endif

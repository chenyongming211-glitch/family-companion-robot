// Application owner only: retain AudioService tasks throughout OTA. The codec
// latch rejects stale producers while flash is in progress.
bool Application::PrepareFamilyFirmwareBootAudio() {
    auto& assets=Assets::GetInstance();
    if(!assets.partition_valid() || !assets.Apply(false))return false;
    assets_version_checked_=true; // existing local models must not be replaced after AFE starts
    ResumeFamilyFirmwareInput();return true;
}
bool Application::FamilyFirmwareEligible() {
    auto codec=Board::GetInstance().GetAudioCodec();
    if(GetDeviceState()!=kDeviceStateIdle || notify_player_.IsBusy() || audio_service_.IsAudioProcessorRunning() ||
       !audio_service_.IsIdle() || audio_service_.IsAudioTestingRunning() || !audio_service_.IsPlaybackIdle() || codec->PlaybackBusy() || !codec->FirmwareAvailable() || FamilyControl::FirmwareBusy())return false;
#if CONFIG_FAMILY_MUSIC_AUDITION
    if(MusicAudition::Active())return false;
#endif
    return !protocol_ || !protocol_->IsAudioChannelOpened();
}
void Application::PauseFamilyFirmwareInput() {
    audio_service_.SetExperimentUplink(false);
    audio_service_.EnableVoiceProcessing(false);
    audio_service_.EnableWakeWordDetection(false);
}
void Application::ResumeFamilyFirmwareInput() {audio_service_.EnableWakeWordDetection(true);}
bool Application::FamilyFirmwareQuiescent() {return audio_service_.IsIdle() && !audio_service_.IsAudioProcessorRunning();}
bool Application::BeginFamilyFirmware() {
    if(!FamilyFirmwareEligible() || !SetDeviceState(kDeviceStateUpgrading))return false;
    PauseFamilyFirmwareInput();
    audio_service_.ResetDecoder();while(audio_service_.PopPacketFromSendQueue()){}
    auto& board=Board::GetInstance();board.SetPowerSaveLevel(PowerSaveLevel::PERFORMANCE);
    board.GetDisplay()->SetChatMessage("system","固件升级中");return true;
}
void Application::FinishFamilyFirmware() {
    auto& board=Board::GetInstance();board.GetDisplay()->SetChatMessage("system","");
    board.SetPowerSaveLevel(PowerSaveLevel::LOW_POWER);SetDeviceState(kDeviceStateIdle);
}

// The SDK buffer is borrowed until this ordered receive callback returns.
// Shape validation precedes Admit, pool copying, and any packet allocation.
void WebsocketProtocol::DeliverC3PcmAudio(const uint8_t* bytes,size_t size,uint32_t connection) {
    std::lock_guard<std::mutex> lock(playback_mutex_);
    if(connection!=playback_connection_)return;
    auto& audio=Application::GetInstance().GetAudioService();
    auto* codec=Board::GetInstance().GetAudioCodec();
    if(!bytes || size!=2880 || !playback_negotiated_ || !playback_drain_negotiated_ || !playback_receiving_ ||
       !playback_tracking_ || playback_first_>UINT32_MAX-1440) {
        codec->PlaybackControl(AudioCodec::PlaybackOp::Invalidate,playback_id_);
        audio.RequestC3PcmFault(audio.C3Uplink().Capture());return;
    }
    if(!codec->PlaybackControl(AudioCodec::PlaybackOp::Admit,playback_id_,playback_first_,1440)) {
        audio.RequestC3PcmFault(audio.C3Uplink().Capture());return;
    }
    const family_robot::C3PcmOutputMetadata metadata{playback_id_.connection,playback_id_.playback,
        static_cast<uint32_t>(playback_first_),codec->PlaybackGeneration()};
    if(audio.PushC3PcmOutput(bytes,size,metadata))playback_first_+=1440;
}

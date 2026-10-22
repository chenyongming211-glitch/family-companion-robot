// Production protocol methods, shared with the host integration harness.

bool WebsocketProtocol::ResetPlaybackConnection(uint32_t expected, uint32_t* closed_epoch) {
    std::lock_guard<std::mutex> lock(playback_mutex_);
    if (expected && expected != playback_connection_) return false;
    // Never reuse a saturated epoch for another socket or close notification.
    if (playback_connection_ == UINT32_MAX || (!expected && playback_connection_ == UINT32_MAX - 1)) return false;
    // Saturation permanently disables receipts on this protocol object.
    if (playback_connection_ != UINT32_MAX) ++playback_connection_;
    if (closed_epoch) *closed_epoch = playback_connection_;
    playback_negotiated_ = playback_receiving_ = playback_tracking_ = false;
    playback_id_ = {}; playback_first_ = 0;
    session_id_.clear();
    auto* codec = Board::GetInstance().GetAudioCodec();
    Application::GetInstance().GetAudioService().ResetDecoder();
    codec->PlaybackControl(AudioCodec::PlaybackOp::Connect, {playback_connection_,0});
    Application::GetInstance().GetAudioService().PreserveServerPlayback(false);
    return true;
}
bool WebsocketProtocol::PlaybackNegotiated() const {
    std::lock_guard<std::mutex> lock(playback_mutex_); return playback_negotiated_;
}
family_robot::PlaybackEvent WebsocketProtocol::PlaybackEventIdentity(const cJSON* root) const {
    // Metadata added only by the validated receive callback, not a later read of
    // current protocol state. Disconnect/new start cannot relabel scheduled UI.
    auto* connection = cJSON_GetObjectItem(root, "_device_playback_connection");
    auto* id = cJSON_GetObjectItem(root, "playback_id");
    auto* lease = cJSON_GetObjectItem(root, "_device_playback_lease");
    if (!cJSON_IsNumber(connection) || !cJSON_IsNumber(id) || !cJSON_IsNumber(lease)) return {};
    return {{static_cast<uint32_t>(connection->valuedouble), static_cast<uint32_t>(id->valuedouble)},
            static_cast<uint32_t>(lease->valuedouble)};
}
bool WebsocketProtocol::PlaybackEventCurrent(family_robot::PlaybackEvent event) const {
    std::lock_guard<std::mutex> lock(playback_mutex_);
    return playback_negotiated_ && event.identity == playback_id_ &&
        event.lease == playback_event_lease_ && event.lease != UINT32_MAX;
}
bool WebsocketProtocol::HandlePlaybackTts(cJSON* root, uint32_t connection) {
    std::lock_guard<std::mutex> lock(playback_mutex_);
    if (connection != playback_connection_) return false;
    cJSON_DeleteItemFromObject(root, "_device_playback_connection");
    cJSON_DeleteItemFromObject(root, "_device_playback_lease");
    if (!playback_negotiated_) return true;
    auto* session = cJSON_GetObjectItem(root, "session_id");
    auto* number = cJSON_GetObjectItem(root, "playback_id");
    auto* state = cJSON_GetObjectItem(root, "state");
    if (!cJSON_IsString(session) || session_id_ != session->valuestring || !cJSON_IsString(state) ||
        !cJSON_IsNumber(number) || !std::isfinite(number->valuedouble) || number->valuedouble < 1 ||
        number->valuedouble > UINT32_MAX || std::floor(number->valuedouble) != number->valuedouble) return false;
    const family_robot::PlaybackIdentity id{playback_connection_, static_cast<uint32_t>(number->valuedouble)};
    auto* codec = Board::GetInstance().GetAudioCodec();
    if (strcmp(state->valuestring, "start") == 0) {
        if (id.playback <= playback_id_.playback) return false;
        // Runs before the next binary frame on this ordered WebSocket. A later
        // scheduled UI start may not reset the decoder or relabel these frames.
        if (!playback_id_ || playback_receiving_)
            Application::GetInstance().GetAudioService().ResetDecoder();
        if (playback_event_lease_ != UINT32_MAX) ++playback_event_lease_;
        playback_id_ = id; playback_first_ = 0; playback_receiving_ = true;
        playback_tracking_ = codec->PlaybackControl(AudioCodec::PlaybackOp::Start, id);
    } else if (id != playback_id_) return false;
    else if (strcmp(state->valuestring, "stop") == 0) {
        playback_receiving_ = false;
        auto* reason = cJSON_GetObjectItem(root, "reason");
        const bool interrupted = cJSON_IsTrue(cJSON_GetObjectItem(root, "interrupted")) ||
            (cJSON_IsString(reason) && strcmp(reason->valuestring, "interrupted") == 0);
        if (interrupted) {
            if (playback_event_lease_ != UINT32_MAX) ++playback_event_lease_;
            Application::GetInstance().GetAudioService().ResetDecoder();
        } else codec->PlaybackControl(AudioCodec::PlaybackOp::Seal, id);
    }
    cJSON_AddNumberToObject(root, "_device_playback_connection", connection);
    cJSON_AddNumberToObject(root, "_device_playback_lease", playback_event_lease_);
    return true;
}
void WebsocketProtocol::DeliverAudio(std::unique_ptr<AudioStreamPacket> packet, uint32_t connection) {
    {
        std::lock_guard<std::mutex> lock(playback_mutex_);
        if (connection != playback_connection_) return;
        if (playback_negotiated_) {
            if (!playback_receiving_) return;
            packet->receipt_id = {playback_connection_, 0};
            packet->output_generation = Board::GetInstance().GetAudioCodec()->PlaybackGeneration();
            if (playback_first_ > UINT32_MAX - 1440) {
                Board::GetInstance().GetAudioCodec()->PlaybackControl(AudioCodec::PlaybackOp::Invalidate, playback_id_);
                // Continue admitted-work accounting, but compact frame offsets
                // are no longer exact and this block can never become valid again.
            }
            if (!playback_tracking_) {
                // Capacity exhaustion degrades to ordinary speech with no receipt.
                // Output still carries the cancellation generation.
            } else {
                packet->receipt_id = playback_id_;
                packet->receipt_first = static_cast<uint32_t>(playback_first_);
                auto* codec = Board::GetInstance().GetAudioCodec();
                if (packet->sample_rate != 24000 || packet->frame_duration != 60)
                    codec->PlaybackControl(AudioCodec::PlaybackOp::Invalidate, playback_id_);
                if (!codec->PlaybackControl(AudioCodec::PlaybackOp::Admit, playback_id_, playback_first_, 1440)) {
                    codec->PlaybackControl(AudioCodec::PlaybackOp::Invalidate, playback_id_);
                    // No tracking record/cancelled block: never relabel as a prompt.
                    return;
                }
                playback_first_ += 1440;
            }
        }
    }
    if (on_incoming_audio_) on_incoming_audio_(std::move(packet));
}
void WebsocketProtocol::SendPlayback(const family_robot::PlaybackSnapshot& r) {
    char session[65]{};
    {
        std::lock_guard<std::mutex> lock(playback_mutex_);
        if (!playback_negotiated_ || r.identity.connection != playback_connection_ ||
            !family_robot::PlaybackSessionSupported(session_id_.data(), session_id_.size())) return;
        memcpy(session, session_id_.data(), session_id_.size());
    }
    // Main owns Open/Close. A disconnect after this check can only send to the
    // dying socket, never the next connection. No heap JSON tree or report queue.
    char escaped[129]{};
    size_t n = 0;
    for (char value : session) {
        if (!value) break;
        if (value == '\\' || value == '\"') escaped[n++] = '\\';
        escaped[n++] = value;
    }
    char message[512];
    const int length = snprintf(message, sizeof(message),
        "{\"type\":\"playback\",\"state\":\"%s\",\"session_id\":\"%s\","
        "\"playback_id\":%lu,\"sample_rate\":24000,\"played_samples\":%llu,"
        "\"valid\":%s,\"basis\":\"i2s_dma\"%s}",
        r.finished ? "finished" : "progress", escaped, static_cast<unsigned long>(r.identity.playback),
        static_cast<unsigned long long>(r.played_samples), r.valid ? "true" : "false",
        r.finished ? (r.interrupted ? ",\"interrupted\":true" : ",\"interrupted\":false") : "");
    if (length > 0 && static_cast<size_t>(length) < sizeof(message)) SendText(message);
}

void WebsocketProtocol::SendAbortSpeaking(AbortReason reason) {
    {
        std::lock_guard<std::mutex> lock(playback_mutex_);
        if (playback_negotiated_) {
            playback_receiving_ = false;
            if (playback_event_lease_ != UINT32_MAX) ++playback_event_lease_;
        }
    }
    Protocol::SendAbortSpeaking(reason);
}

void WebsocketProtocol::ParseServerHello(const cJSON* root, uint32_t connection) {
    auto transport = cJSON_GetObjectItem(root, "transport");
    if (!cJSON_IsString(transport)) {
        ESP_LOGE(TAG, "Missing or non-string transport in server hello");
        return;
    }
    if (strcmp(transport->valuestring, "websocket") != 0) {
        ESP_LOGE(TAG, "Unsupported transport: %s", transport->valuestring);
        return;
    }

    std::lock_guard<std::mutex> playback_lock(playback_mutex_);
    if (connection != playback_connection_) return;
    auto session_id = cJSON_GetObjectItem(root, "session_id");
    if (cJSON_IsString(session_id)) {
        session_id_ = session_id->valuestring;
#if !CONFIG_STOPWATCH_SOFTWARE_AEC
        ESP_LOGI(TAG, "Session ID: %s", session_id_.c_str());
#endif
    }

    auto audio_params = cJSON_GetObjectItem(root, "audio_params");
    if (cJSON_IsObject(audio_params)) {
        auto sample_rate = cJSON_GetObjectItem(audio_params, "sample_rate");
        if (cJSON_IsNumber(sample_rate)) {
            server_sample_rate_ = sample_rate->valueint;
        }
        auto frame_duration = cJSON_GetObjectItem(audio_params, "frame_duration");
        if (cJSON_IsNumber(frame_duration)) {
            server_frame_duration_ = frame_duration->valueint;
        }
    }

    auto* features = cJSON_GetObjectItem(root, "features");
    playback_negotiated_ = playback_connection_ != 0 && playback_connection_ != UINT32_MAX && cJSON_IsString(session_id) &&
        family_robot::PlaybackSessionSupported(session_id_.data(), session_id_.size()) && Board::GetInstance().GetAudioCodec()->PlaybackSupported() &&
        cJSON_IsTrue(cJSON_GetObjectItem(features, "playback_progress_v1"));
    Application::GetInstance().GetAudioService().PreserveServerPlayback(playback_negotiated_);
    xEventGroupSetBits(event_group_handle_, WEBSOCKET_PROTOCOL_SERVER_HELLO_EVENT);
}

uint32_t WebsocketProtocol::AudioChannelEpoch() const {
    std::lock_guard<std::mutex> lock(playback_mutex_);
    return playback_connection_;
}
void WebsocketProtocol::NotifyPlaybackClosed(uint32_t connection) {
    uint32_t closed_epoch;
    if (!ResetPlaybackConnection(connection, &closed_epoch)) return;
    if (on_audio_channel_closed_) on_audio_channel_closed_(closed_epoch);
}
void WebsocketProtocol::CloseAudioChannel(bool send_goodbye) {
    (void)send_goodbye;
    if (!websocket_) return;
    // Claim this physical socket's close before destruction. Synchronous, delayed
    // and repeated transport callbacks then fail the old-epoch check harmlessly.
    NotifyPlaybackClosed(socket_connection_);
    websocket_.reset();
}

// Shared production initialization and fault-injection test implementation.
void SoftwareAecCodec::InitializePlayback(int input_rate, int output_rate) {
    input_channels_ = 2;
    input_reference_ = true;  // ONLY logical format; esp_codec_dev_open stays mono.
    auto* storage = heap_caps_malloc(sizeof(D), MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (storage)
        pair_ = new (storage) D;
    auto* playback_storage = heap_caps_malloc(sizeof(family_robot::PlaybackProgress), MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (playback_storage) playback_ = new (playback_storage) family_robot::PlaybackProgress;
    const auto tx_disabled = i2s_channel_disable(tx_handle_);
    const auto rx_disabled = i2s_channel_disable(rx_handle_);
    if (pair_ && playback_ && tx_disabled == ESP_OK && rx_disabled == ESP_OK && input_rate == D::kRate &&
        output_rate == D::kRate) {
        i2s_event_callbacks_t tx_callbacks = {}, rx_callbacks = {};
        tx_callbacks.on_sent = Sent;
        rx_callbacks.on_recv = Received;
        callbacks_ok_ =
            i2s_channel_register_event_callback(tx_handle_, &tx_callbacks, this) == ESP_OK &&
            i2s_channel_register_event_callback(rx_handle_, &rx_callbacks, this) == ESP_OK;
    }
    // Initial constructor format is stereo. Ignore callbacks until mono open hook.
    const auto tx_enabled = i2s_channel_enable(tx_handle_);
    const auto rx_enabled = i2s_channel_enable(rx_handle_);
    if (!callbacks_ok_ || tx_enabled != ESP_OK || rx_enabled != ESP_OK)
        UseOrdinaryOutput();
}
void SoftwareAecCodec::StartPlaybackInfrastructure() {
    if (!refill_mode_) return;
    void* storage = heap_caps_malloc(sizeof(AecDiagnostics), MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (storage) diagnostics_ = new (storage) AecDiagnostics(*this);
    infrastructure_ok_ = diagnostics_ && diagnostics_->Start();
    if (!infrastructure_ok_) UseOrdinaryOutput();
}
void SoftwareAecCodec::UseOrdinaryOutput() {
    // Only during construction/Start, before AudioService starts any producers.
    // Stop acceptance under the ISR lock before deleting its physical channels.
    portENTER_CRITICAL(&mux_);
    accepting_ = false;
    portEXIT_CRITICAL(&mux_);
    Fail(D::Fault::Setup);
    RecreateDuplexForOrdinaryWrite();
    // No callback can exist on the new channels. Publish immutable output mode
    // before audio producer tasks, AFE initialization and capability negotiation.
    refill_mode_ = false;
    callbacks_ok_ = false;
    input_channels_ = 1;
    input_reference_ = false;
}

// These production methods also compile unchanged in the host fault harness.
XiaomanBoxCodec::XiaomanBoxCodec(void* bus, int input_rate, int output_rate, gpio_num_t mclk,
    gpio_num_t bclk, gpio_num_t ws, gpio_num_t dout, gpio_num_t din, gpio_num_t pa,
    uint8_t dac, uint8_t adc, bool reference)
    : BoxAudioCodec(bus,input_rate,output_rate,mclk,bclk,ws,dout,din,pa,dac,adc,reference,
                    30.0f,-1,0.0f,true) {
    InitializePlayback(input_rate,output_rate,reference);
}
XiaomanBoxCodec::~XiaomanBoxCodec() {
    Disarm();
    i2s_channel_disable(tx_handle_);
    i2s_event_callbacks_t callbacks{};
    i2s_channel_register_event_callback(tx_handle_,&callbacks,nullptr);
    if(playback_) {playback_->~PlaybackProgress();heap_caps_free(playback_);}
}
void XiaomanBoxCodec::InitializePlayback(int input_rate,int output_rate,bool reference) {
    auto* memory=heap_caps_malloc(sizeof(family_robot::PlaybackProgress),MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    if(memory) playback_=new(memory) family_robot::PlaybackProgress;
    const auto tx_off=i2s_channel_disable(tx_handle_);
    const auto rx_off=i2s_channel_disable(rx_handle_);
    if(playback_ && tx_off==ESP_OK && rx_off==ESP_OK && reference && input_rate==24000 && output_rate==24000) {
        i2s_event_callbacks_t callbacks{};callbacks.on_sent=Sent;
        callbacks_ok_=i2s_channel_register_event_callback(tx_handle_,&callbacks,this)==ESP_OK;
    }
    const auto tx_on=i2s_channel_enable(tx_handle_);
    const auto rx_on=i2s_channel_enable(rx_handle_);
    setup_failed_=!callbacks_ok_ || tx_on!=ESP_OK || rx_on!=ESP_OK;
    if(setup_failed_) ReportReferenceFailure(); // Never fall back to ordinary Write.
}
void XiaomanBoxCodec::Start() {
    std::lock_guard<std::mutex> lock(lifecycle_);
    AudioCodec::Start(); // persisted volume must be loaded before either codec open
    PreparePlaybackHardware();
}
bool XiaomanBoxCodec::VerifyHardware() {
    i2s_chan_info_t tx{},rx{};
    const auto tx_result=i2s_channel_get_info(tx_handle_,&tx);
    const auto rx_result=i2s_channel_get_info(rx_handle_,&rx);
#ifdef ESP_PLATFORM
    ESP_LOGI("XiaomanBox", "driver in=%d out=%d get=%d/%d tx[dir=%d mode=%d enabled=%d bytes=%u bclk=%u mclk=%u] rx[dir=%d mode=%d enabled=%d bytes=%u bclk=%u mclk=%u]",
        input_enabled_,output_enabled_,tx_result,rx_result,tx.dir,tx.mode,tx.is_enabled,
        unsigned(tx.total_dma_buf_size),unsigned(tx.bclk_hz),unsigned(tx.mclk_hz),
        rx.dir,rx.mode,rx.is_enabled,unsigned(rx.total_dma_buf_size),unsigned(rx.bclk_hz),unsigned(rx.mclk_hz));
#endif
    if(tx_result!=ESP_OK || rx_result!=ESP_OK ||
       tx.dir!=I2S_DIR_TX || rx.dir!=I2S_DIR_RX || tx.mode!=I2S_COMM_MODE_STD || rx.mode!=I2S_COMM_MODE_TDM ||
       tx.pair_chan!=rx_handle_ || rx.pair_chan!=tx_handle_ ||
       tx.is_enabled!=(output_enabled_ || input_enabled_) || rx.is_enabled!=input_enabled_ || !tx.mode_cfg || !rx.mode_cfg)
        return false;
    // IDF6.1 public readback is configured state/calculated clocks, not a
    // physical BCLK measurement. Read pointed-to configs only under lifecycle_.
    const auto& out=*static_cast<const i2s_std_config_t*>(tx.mode_cfg);
    const auto& in=*static_cast<const i2s_tdm_config_t*>(rx.mode_cfg);
#ifdef ESP_PLATFORM
    ESP_LOGI("XiaomanBox", "format tx[rate=%u data=%d slot=%d mode=%d mask=%u] rx[rate=%u data=%d slot=%d mode=%d mask=%u total=%u]",
        unsigned(out.clk_cfg.sample_rate_hz),out.slot_cfg.data_bit_width,out.slot_cfg.slot_bit_width,
        out.slot_cfg.slot_mode,unsigned(out.slot_cfg.slot_mask),unsigned(in.clk_cfg.sample_rate_hz),
        in.slot_cfg.data_bit_width,in.slot_cfg.slot_bit_width,in.slot_cfg.slot_mode,
        unsigned(in.slot_cfg.slot_mask),unsigned(in.slot_cfg.total_slot));
#endif
    const int tx_slot=out.slot_cfg.slot_bit_width;
    if((tx_slot!=I2S_SLOT_BIT_WIDTH_16BIT && tx_slot!=I2S_SLOT_BIT_WIDTH_32BIT) ||
       (input_enabled_ && tx_slot!=I2S_SLOT_BIT_WIDTH_32BIT))return false;
    if(output_enabled_ && (tx.total_dma_buf_size!=2880 || out.clk_cfg.sample_rate_hz!=24000 ||
       out.slot_cfg.data_bit_width!=I2S_DATA_BIT_WIDTH_16BIT || out.slot_cfg.slot_bit_width!=tx_slot ||
       out.slot_cfg.slot_mode!=I2S_SLOT_MODE_MONO || out.slot_cfg.slot_mask!=I2S_STD_SLOT_LEFT ||
       tx.bclk_hz!=24000u*2u*static_cast<unsigned>(tx_slot) || tx.mclk_hz!=24000u*256u)) return false;
    if(input_enabled_ && (rx.total_dma_buf_size!=5760 || in.clk_cfg.sample_rate_hz!=24000 ||
       in.slot_cfg.data_bit_width!=I2S_DATA_BIT_WIDTH_16BIT || in.slot_cfg.slot_bit_width!=I2S_SLOT_BIT_WIDTH_16BIT ||
       in.slot_cfg.slot_mode!=I2S_SLOT_MODE_STEREO || in.slot_cfg.slot_mask!=(I2S_TDM_SLOT0|I2S_TDM_SLOT1) ||
       in.slot_cfg.total_slot!=4 || rx.bclk_hz!=24000u*4u*16u || rx.mclk_hz!=24000u*256u)) return false;
    return true;
}
void XiaomanBoxCodec::FaultLocked(uint32_t source) {
    accepting_=false;aec_ready_=UINT32_MAX;
    if(!faulted_) {fault_source_=source;faulted_=true;epochs_.Fault();}
    if(playback_) playback_->HardwareFault();
}
void XiaomanBoxCodec::Disarm() {
    portENTER_CRITICAL(&mux_);
    accepting_=false;aec_ready_=UINT32_MAX;epochs_.HardwareChange();
    if(!faulted_) {last_dma_us_=last_dma_gap_us_=0;last_dma_bytes_=0;}
    if(output_generation_!=UINT32_MAX) ++output_generation_;
    if(playback_) {playback_->HardwareFault();playback_->HardwareReset();}
    portEXIT_CRITICAL(&mux_);
}
void XiaomanBoxCodec::Arm() {
    const bool verified=VerifyHardware();
    portENTER_CRITICAL(&mux_);
    if(!verified || setup_failed_ || !callbacks_ok_ || output_generation_==UINT32_MAX) FaultLocked(1);
    else accepting_=output_enabled_ && input_enabled_ && !faulted_;
    portEXIT_CRITICAL(&mux_);
}
bool XiaomanBoxCodec::PreparePlaybackHardware() {
    if(ReferenceFaulted() || !callbacks_ok_) return false;
    if(!input_enabled_ || !output_enabled_) {
        Disarm();
        // Hardware MR clock negotiation requires ADC open before the mono DAC.
        if(!input_enabled_) BoxAudioCodec::EnableInput(true);
        if(!output_enabled_) BoxAudioCodec::EnableOutput(true);
        Arm();
    }
    return PlaybackSupported() && ReferenceValid();
}
void XiaomanBoxCodec::EnableInput(bool enable) {
    std::lock_guard<std::mutex> lock(lifecycle_);
    if(enable==input_enabled_) return;
    Disarm();
    BoxAudioCodec::EnableInput(enable);
    // ReadAudioData normally reopens only IN after power idle. Complete the
    // healthy MR pair before arming, using the same ADC-before-DAC order as
    // initial Start. A latched fault never becomes an automatic recovery.
    if(enable && !output_enabled_ && !ReferenceFaulted())
        BoxAudioCodec::EnableOutput(true);
    Arm();
}
void XiaomanBoxCodec::EnableOutput(bool enable) {
    std::lock_guard<std::mutex> lock(lifecycle_);
    if(enable==output_enabled_) return;
    Disarm();BoxAudioCodec::EnableOutput(enable);Arm();
}
bool XiaomanBoxCodec::InputData(std::vector<int16_t>& data) {
    std::lock_guard<std::mutex> lock(lifecycle_);
    const auto generation=CaptureGeneration();
#ifdef ESP_PLATFORM
    // Task-only diagnostics; never format or log from a DMA callback.
    portENTER_CRITICAL(&mux_);
    const bool report=faulted_ && !fault_logged_;
    const auto source=fault_source_;
    const auto bytes=last_dma_bytes_;
    const auto gap=last_dma_gap_us_;
    if(report)fault_logged_=true;
    portEXIT_CRITICAL(&mux_);
    if(report)ESP_LOGE("XiaomanBox", "capture blocked source=%u dma_bytes=%u dma_gap_us=%lld",
        unsigned(source),unsigned(bytes),static_cast<long long>(gap));
#endif
    if(!input_enabled_ || ReferenceFaulted()) return false;
    // callback_refill's checked Box Read returns zero on codec-device failure.
#ifdef ESP_PLATFORM
    const bool probe=!read_probe_done_;
    read_probe_done_=true;
    if(probe) {
        portENTER_CRITICAL(&mux_);
        const auto bytes=last_dma_bytes_;
        const auto gap=last_dma_gap_us_;
        const auto stamp=last_dma_us_;
        portEXIT_CRITICAL(&mux_);
        ESP_LOGI("XiaomanBox", "first input read begin samples=%u capture=%u dma_bytes=%u gap_us=%lld age_us=%lld",
            unsigned(data.size()),unsigned(generation),unsigned(bytes),static_cast<long long>(gap),
            static_cast<long long>(stamp?esp_timer_get_time()-stamp:-1));
    }
#endif
    const bool read=BoxAudioCodec::InputData(data);
#ifdef ESP_PLATFORM
    if(probe)ESP_LOGI("XiaomanBox", "first input read end ok=%d capture=%u current=%u",
        read,unsigned(generation),unsigned(CaptureGeneration()));
#endif
    if(!read) ReportReferenceFailure();
    return read && generation==CaptureGeneration();
}
bool XiaomanBoxCodec::PlaybackSupported() const {
    // Negotiable capability survives intentional power idle. Start separately
    // reopens/verifies hardware before creating any trusted playback record.
    portENTER_CRITICAL(&mux_);const bool ok=playback_ && callbacks_ok_ && !setup_failed_ && !faulted_;
    portEXIT_CRITICAL(&mux_);return ok;
}
bool XiaomanBoxCodec::ReferenceValid() const {
    portENTER_CRITICAL(&mux_);const bool ok=accepting_ && !faulted_;
#ifdef ESP_PLATFORM
    // A blocked ADC read cannot report a later ISR fault. AFE calls this
    // task-only snapshot independently, so preserve/report the first fault here.
    const bool report=faulted_ && !fault_logged_;
    const auto source=fault_source_;
    const auto bytes=last_dma_bytes_;
    const auto gap=last_dma_gap_us_;
    if(report)fault_logged_=true;
#endif
    portEXIT_CRITICAL(&mux_);
#ifdef ESP_PLATFORM
    if(report)ESP_LOGE("XiaomanBox", "capture blocked source=%u dma_bytes=%u dma_gap_us=%lld",
        unsigned(source),unsigned(bytes),static_cast<long long>(gap));
#endif
    return ok;
}
bool XiaomanBoxCodec::ReferenceFaulted() const {
    portENTER_CRITICAL(&mux_);const bool fault=faulted_;portEXIT_CRITICAL(&mux_);return fault;
}
bool XiaomanBoxCodec::RealtimeEligible() const {
    portENTER_CRITICAL(&mux_);
    const bool ok=accepting_ && !faulted_ && capture_active_ && aec_ready_==epochs_.capture();
    portEXIT_CRITICAL(&mux_);return ok;
}
void XiaomanBoxCodec::NotifyAecReady(bool ready,uint32_t generation) {
    portENTER_CRITICAL(&mux_);
    aec_ready_=ready && accepting_ && !faulted_ && capture_active_ && generation==epochs_.capture()?generation:UINT32_MAX;
    portEXIT_CRITICAL(&mux_);
}
void XiaomanBoxCodec::ReportReferenceFailure() {portENTER_CRITICAL(&mux_);FaultLocked();portEXIT_CRITICAL(&mux_);}
uint32_t XiaomanBoxCodec::CaptureGeneration() const {portENTER_CRITICAL(&mux_);const auto n=epochs_.capture();portEXIT_CRITICAL(&mux_);return n;}
uint32_t XiaomanBoxCodec::WakeAudioGeneration() const {portENTER_CRITICAL(&mux_);const auto n=epochs_.wake();portEXIT_CRITICAL(&mux_);return n;}
uint32_t XiaomanBoxCodec::ReferenceIntegrity() const {portENTER_CRITICAL(&mux_);const auto n=epochs_.integrity();portEXIT_CRITICAL(&mux_);return n;}
void XiaomanBoxCodec::SetCaptureActive(bool active) {
    portENTER_CRITICAL(&mux_);
    if(capture_active_!=active) {capture_active_=active;aec_ready_=UINT32_MAX;if(active)epochs_.Resume();else epochs_.Pause();}
    portEXIT_CRITICAL(&mux_);
}
void XiaomanBoxCodec::CaptureBoundary() {portENTER_CRITICAL(&mux_);epochs_.Boundary();aec_ready_=UINT32_MAX;portEXIT_CRITICAL(&mux_);}

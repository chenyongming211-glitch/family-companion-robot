// Compiled unchanged by the production .cc and host fault harness.
FamilyC3Codec::FamilyC3Codec(void* bus,i2c_port_t port,int input_rate,int output_rate,
    gpio_num_t mclk,gpio_num_t bclk,gpio_num_t ws,gpio_num_t dout,gpio_num_t din,
    gpio_num_t pa,uint8_t addr,bool use_mclk,bool inverted)
    : Es8311AudioCodec(bus,port,input_rate,output_rate,mclk,bclk,ws,dout,din,pa,addr,
                       use_mclk,inverted,true),fault_pa_pin_(pa),fault_pa_inverted_(inverted) {
    InitializePlayback(input_rate,output_rate);
}
FamilyC3Codec::~FamilyC3Codec() {
    std::lock_guard<std::mutex> lock(lifecycle_);
    portENTER_CRITICAL(&mux_);accepting_=false;
#if CONFIG_XMINI_C3_VOICE_BARGE
    reference_callbacks_armed_=false;capture_active_=false;ResetCaptureLocked();
#endif
    portEXIT_CRITICAL(&mux_);
    // Channel disable synchronizes against callbacks before their context dies.
#if CONFIG_XMINI_C3_VOICE_BARGE
    for(auto* channel:{tx_handle_,rx_handle_}) {
        i2s_chan_info_t info{};
        if(i2s_channel_get_info(channel,&info)!=ESP_OK ||
           (info.is_enabled && i2s_channel_disable(channel)!=ESP_OK))std::terminate();
    }
    i2s_event_callbacks_t empty{};
    if(tx_callback_registered_ && i2s_channel_register_event_callback(tx_handle_,&empty,nullptr)!=ESP_OK)std::terminate();
    if(rx_callback_registered_ && i2s_channel_register_event_callback(rx_handle_,&empty,nullptr)!=ESP_OK)std::terminate();
    if(pair_) {pair_->~Pair();heap_caps_free(pair_);}
#else
    if(callbacks_ok_) {
        i2s_chan_info_t info{};
        // A callback context must never be freed while a running interrupt can
        // still use it. Unexpected teardown failure cannot safely continue.
        if(i2s_channel_get_info(tx_handle_,&info)!=ESP_OK ||
           (info.is_enabled && i2s_channel_disable(tx_handle_)!=ESP_OK))std::terminate();
        i2s_event_callbacks_t callbacks{};
        if(i2s_channel_register_event_callback(tx_handle_,&callbacks,nullptr)!=ESP_OK)std::terminate();
    }
#endif
    if(playback_) {playback_->~PlaybackProgress();heap_caps_free(playback_);}
}
void FamilyC3Codec::InitializePlayback(int input_rate,int output_rate) {
#if CONFIG_XMINI_C3_VOICE_BARGE
    input_reference_=true;input_channels_=2; // logical MR only; ES8311 stays mono
    auto* pair_memory=heap_caps_malloc(sizeof(Pair),MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    if(pair_memory)pair_=new(pair_memory) Pair;
#endif
    auto* memory=heap_caps_malloc(sizeof(family_robot::PlaybackProgress),MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    if(memory)playback_=new(memory) family_robot::PlaybackProgress;
    const auto tx_off=i2s_channel_disable(tx_handle_);
    const auto rx_off=i2s_channel_disable(rx_handle_);
    if(playback_
#if CONFIG_XMINI_C3_VOICE_BARGE
       && pair_
#endif
       && input_rate==24000 && output_rate==24000 && tx_off==ESP_OK && rx_off==ESP_OK) {
        i2s_event_callbacks_t callbacks{};callbacks.on_sent=Sent;
        callbacks_ok_=i2s_channel_register_event_callback(tx_handle_,&callbacks,this)==ESP_OK;
#if CONFIG_XMINI_C3_VOICE_BARGE
        tx_callback_registered_=callbacks_ok_;
        i2s_event_callbacks_t rx_callbacks{};rx_callbacks.on_recv=Received;
        rx_callback_registered_=i2s_channel_register_event_callback(rx_handle_,&rx_callbacks,this)==ESP_OK;
        callbacks_ok_=callbacks_ok_ && rx_callback_registered_;
#endif
    }
    const auto tx_on=i2s_channel_enable(tx_handle_);
    const auto rx_on=i2s_channel_enable(rx_handle_);
    setup_failed_=!callbacks_ok_ || tx_on!=ESP_OK || rx_on!=ESP_OK;
    if(setup_failed_) {portENTER_CRITICAL(&mux_);FaultLocked();portEXIT_CRITICAL(&mux_);}
}
void FamilyC3Codec::FaultLocked() {
#if CONFIG_XMINI_C3_VOICE_BARGE
    if(!faulted_) {capture_active_=false;ResetCaptureLocked();}
    reference_callbacks_armed_=false;reference_valid_=false;aec_ready_generation_=UINT32_MAX;
    if(pair_)pair_->Fail(Pair::Fault::Setup);
#endif
    faulted_=true;accepting_=false;
    if(playback_)playback_->HardwareFault();
}
void FamilyC3Codec::AdvanceGenerationLocked() {
    if(output_generation_>=UINT32_MAX-1) {output_generation_=UINT32_MAX;FaultLocked();}
    else ++output_generation_;
}
void FamilyC3Codec::DisarmLocked() {
    accepting_=false;pending_since_us_=last_tx_us_=0;AdvanceGenerationLocked();
    if(playback_) {
        playback_->InterruptAll();
        // InterruptAll removes software work; Busy now means physical DMA tags.
        if(playback_->Busy() && !scrub_pending_) {
            scrub_pending_=true;scrubbed_=0;
            for(auto& address:scrub_addresses_)address=nullptr;
        }
        playback_->HardwareFault();playback_->HardwareReset();
    }
}
void FamilyC3Codec::StopFaultedOutput() {
    portENTER_CRITICAL(&mux_);const bool stop=faulted_ && !fault_stop_done_;portEXIT_CRITICAL(&mux_);
    if(!stop)return;
    // Mute before trying any task lock. A producer or bounded capture read may
    // own lifecycle_; losing callbacks must never leave old DMA audible while
    // the main task waits for that owner to finish.
    if(fault_pa_pin_!=GPIO_NUM_NC)gpio_set_level(fault_pa_pin_,fault_pa_inverted_?1:0);
    std::unique_lock<std::mutex> lock(lifecycle_,std::try_to_lock);
    if(!lock.owns_lock())return; // next regular PlaybackPoll retries the close
    portENTER_CRITICAL(&mux_);const bool was_open=hardware_open_;portEXIT_CRITICAL(&mux_);
    // Use codec close so its internal I2S enable bookkeeping stays consistent.
    // No adapter read or TX writer can hold a channel semaphore under this
    // exclusive lifecycle lock; callback-refill has no ordinary TX writer.
    Es8311AudioCodec::EnableOutput(false);
    Es8311AudioCodec::EnableInput(false);
    if(was_open)ClearClosedDescriptors();
    portENTER_CRITICAL(&mux_);
    fault_stop_done_=!hardware_open_ && !input_enabled_ && !output_enabled_;
    portEXIT_CRITICAL(&mux_);
}
bool FamilyC3Codec::VerifyHardware() {
    i2s_chan_info_t tx{},rx{};
    if(i2s_channel_get_info(tx_handle_,&tx)!=ESP_OK || i2s_channel_get_info(rx_handle_,&rx)!=ESP_OK)
        return false;
    const bool running=input_enabled_||output_enabled_;
    if(tx.dir!=I2S_DIR_TX || rx.dir!=I2S_DIR_RX || tx.mode!=I2S_COMM_MODE_STD || rx.mode!=I2S_COMM_MODE_STD ||
       tx.pair_chan!=rx_handle_ || rx.pair_chan!=tx_handle_ ||
       tx.is_enabled!=running || rx.is_enabled!=running || !tx.mode_cfg || !rx.mode_cfg)return false;
    // IDF6.1 readback is configured state/calculated clocks, not a physical
    // clock measurement. Pinned IDF C3 uses I2S HW2 (soc_caps.h), normalizes
    // AUTO to 16-bit slots. STD has two wire slots even though mono DMA
    // contains one 16-bit sample per frame.
    for(const auto* info:{&tx,&rx}) {
        const auto& config=*static_cast<const i2s_std_config_t*>(info->mode_cfg);
        if(info->total_dma_buf_size!=family_robot::PlaybackProgress::kSlots*480 ||
           info->bclk_hz!=24000u*2u*16u || info->mclk_hz!=24000u*256u ||
           config.clk_cfg.sample_rate_hz!=24000 ||
           config.slot_cfg.data_bit_width!=I2S_DATA_BIT_WIDTH_16BIT ||
           config.slot_cfg.slot_bit_width!=I2S_SLOT_BIT_WIDTH_16BIT ||
           config.slot_cfg.slot_mode!=I2S_SLOT_MODE_MONO || config.slot_cfg.slot_mask!=I2S_STD_SLOT_LEFT ||
           config.slot_cfg.ws_width!=16 || config.slot_cfg.ws_pol || !config.slot_cfg.bit_shift)return false;
#if SOC_I2S_HW_VERSION_2
        if(!config.slot_cfg.left_align || config.slot_cfg.big_endian || config.slot_cfg.bit_order_lsb)return false;
#else
#error "Family C3 playback is verified for the pinned C3 I2S HW2 configuration"
#endif
    }
    return true;
}
void FamilyC3Codec::OnDeviceEpoch(bool opened) {
    portENTER_CRITICAL(&mux_);DisarmLocked();hardware_open_=opened;
#if CONFIG_XMINI_C3_VOICE_BARGE
    reference_callbacks_armed_=false;ResetCaptureLocked();
    if(opened && pair_ && !faulted_ && physical_generation_<UINT32_MAX-1) {
        pair_->Begin(++physical_generation_,0);
        reference_tx_us_=reference_rx_us_=esp_timer_get_time();
        discard_tx_=discard_rx_=1; // first partial descriptors cannot anchor clocks
        reference_callbacks_armed_=true;
    } else if(opened)FaultLocked();
    else capture_active_=false;
#endif
    portEXIT_CRITICAL(&mux_);
    if(!opened)return;
    const bool valid=VerifyHardware();
    portENTER_CRITICAL(&mux_);
    if(!valid || !callbacks_ok_ || setup_failed_ || faulted_)FaultLocked();
    else {verified_=true;accepting_=output_enabled_ && !scrub_pending_;}
    portEXIT_CRITICAL(&mux_);
}
void FamilyC3Codec::ClearClosedDescriptors() {
    i2s_chan_info_t tx{},rx{};
    const bool stopped=i2s_channel_get_info(tx_handle_,&tx)==ESP_OK &&
        i2s_channel_get_info(rx_handle_,&rx)==ESP_OK && !tx.is_enabled && !rx.is_enabled;
    portENTER_CRITICAL(&mux_);
    if(!stopped)FaultLocked();
    else {
        // The SDK keeps DMA bytes across disable/enable. Neither callback nor
        // hardware owns these stopped descriptors; erase old PCM before reopen.
        for(size_t i=0;i<known_descriptors_;++i)std::memset(dma_addresses_[i],0,480);
        for(auto& address:dma_addresses_)address=nullptr;
        known_descriptors_=scrubbed_=0;scrub_pending_=false;
    }
    portEXIT_CRITICAL(&mux_);
}
bool FamilyC3Codec::WaitForSilence() {
    const auto started=esp_timer_get_time();
    for(;;) {
        portENTER_CRITICAL(&mux_);const bool pending=scrub_pending_;const bool fault=faulted_;portEXIT_CRITICAL(&mux_);
        if(fault)return false;
        if(!pending)return true;
        if(esp_timer_get_time()-started>200000) {
            portENTER_CRITICAL(&mux_);FaultLocked();portEXIT_CRITICAL(&mux_);return false;
        }
        vTaskDelay(1);
    }
}
bool FamilyC3Codec::PrepareHardware() {
    if(ReferenceFaulted() || !callbacks_ok_)return false;
    if(!WaitForSilence())return false;
    if(!input_enabled_)Es8311AudioCodec::EnableInput(true);
    if(!output_enabled_)Es8311AudioCodec::EnableOutput(true);
    const bool valid=VerifyHardware();
    portENTER_CRITICAL(&mux_);
    if(!valid || !hardware_open_ || !input_enabled_ || !output_enabled_)FaultLocked();
    else accepting_=!faulted_ && !scrub_pending_;
    const bool ready=accepting_;portEXIT_CRITICAL(&mux_);return ready;
}
void FamilyC3Codec::Start() {
    std::lock_guard<std::mutex> lock(lifecycle_);AudioCodec::Start();PrepareHardware();
}
void FamilyC3Codec::EnableInput(bool enable) {
    std::lock_guard<std::mutex> lock(lifecycle_);
    if(enable==input_enabled_)return;
    Es8311AudioCodec::EnableInput(enable);
    if(!input_enabled_ && !output_enabled_)ClearClosedDescriptors();
}
void FamilyC3Codec::EnableOutput(bool enable) {
    std::lock_guard<std::mutex> lock(lifecycle_);
    if(enable==output_enabled_)return;
    portENTER_CRITICAL(&mux_);DisarmLocked();portEXIT_CRITICAL(&mux_);
    if(enable && !WaitForSilence())return;
    Es8311AudioCodec::EnableOutput(enable);
    if(!input_enabled_ && !output_enabled_) {ClearClosedDescriptors();return;}
    const bool valid=VerifyHardware();
    portENTER_CRITICAL(&mux_);
    if(!valid)FaultLocked();else accepting_=enable && !faulted_ && !scrub_pending_;
    portEXIT_CRITICAL(&mux_);
}
int FamilyC3Codec::Read(int16_t* dest,int samples) {
    // C3's upstream patch makes the protected base Read return zero on a codec
    // read error. Its private dev_ is intentionally not accessed here.
    if(!input_enabled_ || samples<=0 || ReferenceFaulted())return 0;
    const int got=Es8311AudioCodec::Read(dest,samples);
    if(got!=samples) {portENTER_CRITICAL(&mux_);FaultLocked();portEXIT_CRITICAL(&mux_);return 0;}
    return got;
}
int FamilyC3Codec::Write(const int16_t*,int) {
    // No untracked writer may ever compete with callback-owned TX descriptors.
    portENTER_CRITICAL(&mux_);FaultLocked();portEXIT_CRITICAL(&mux_);return 0;
}
bool FamilyC3Codec::InputData(std::vector<int16_t>& data) {
#if CONFIG_XMINI_C3_VOICE_BARGE
    return InputPaired(data);
#else
    std::lock_guard<std::mutex> lock(lifecycle_);
    if(data.empty() || data.size()>static_cast<size_t>(INT32_MAX))return false;
    return Read(data.data(),static_cast<int>(data.size()))==static_cast<int>(data.size()) && !ReferenceFaulted();
#endif
}
void FamilyC3Codec::CaptureBoundary() {
#if CONFIG_XMINI_C3_VOICE_BARGE
    portENTER_CRITICAL(&mux_);ResetCaptureLocked();portEXIT_CRITICAL(&mux_);
#else
    std::lock_guard<std::mutex> lock(lifecycle_);
    if(!input_enabled_ || ReferenceFaulted())return;
    bool good=VerifyHardware();int16_t discard[family_robot::PlaybackProgress::kBlock];
    // First drain the software-visible partial descriptor and completed queue.
    // Zero bytes proves that queue empty, but DMA may still be filling a frame
    // that contains samples from before this capture boundary.
    if(good)for(size_t i=0;i<=family_robot::PlaybackProgress::kSlots;++i) {
        size_t bytes=0;
        const auto result=i2s_channel_read(rx_handle_,discard,sizeof(discard),&bytes,0);
        if((result!=ESP_OK && result!=ESP_ERR_TIMEOUT) || bytes>sizeof(discard) || bytes%sizeof(int16_t)) {good=false;break;}
        if(bytes==0) {good=result==ESP_ERR_TIMEOUT;break;}
        if(i==family_robot::PlaybackProgress::kSlots)good=false;
    }
    if(good) {
        size_t bytes=0;
        // Cross the in-progress DMA boundary before accepting fresh voice.
        // This returns when the next complete descriptor arrives; 100 ms is
        // only an error bound, never a fixed mute duration or task sleep.
        const auto result=i2s_channel_read(rx_handle_,discard,sizeof(discard),&bytes,100);
        good=result==ESP_OK && bytes==sizeof(discard);
    }
    if(!good) {portENTER_CRITICAL(&mux_);FaultLocked();portEXIT_CRITICAL(&mux_);}
#endif
}
bool FamilyC3Codec::ReferenceFaulted() const {
    portENTER_CRITICAL(&mux_);const bool fault=faulted_;portEXIT_CRITICAL(&mux_);return fault;
}
bool FamilyC3Codec::PlaybackSupported() const {
    portENTER_CRITICAL(&mux_);
    const bool supported=playback_ && verified_ && callbacks_ok_ && !setup_failed_ && !faulted_;
    portEXIT_CRITICAL(&mux_);return supported;
}

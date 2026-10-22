void Es8311AudioCodec::CreateI2sChannels() {
    i2s_chan_config_t chan_cfg{};
    chan_cfg.id = I2S_NUM_0;
    chan_cfg.role = I2S_ROLE_MASTER;
    chan_cfg.dma_desc_num = AUDIO_CODEC_DMA_DESC_NUM;
    chan_cfg.dma_frame_num = AUDIO_CODEC_DMA_FRAME_NUM;
    chan_cfg.auto_clear_after_cb = !callback_refill_;
    chan_cfg.auto_clear_before_cb = false;
    chan_cfg.intr_priority = 0;
    ESP_ERROR_CHECK(i2s_new_channel(&chan_cfg, &tx_handle_, &rx_handle_));
}
// Initialization only: AudioService has not started producers or opened dev_.
// Destroying both disabled channels removes even a partially registered callback.
// New channels restore the stock driver's automatic DMA clearing and sole writer.
void Es8311AudioCodec::RecreateDuplexForOrdinaryWrite() {
    assert(dev_ == nullptr);
    for (auto handle : {tx_handle_, rx_handle_}) {
        i2s_chan_info_t info{};
        ESP_ERROR_CHECK(i2s_channel_get_info(handle, &info));
        if (info.is_enabled) ESP_ERROR_CHECK(i2s_channel_disable(handle));
    }
    audio_codec_delete_data_if(data_if_);
    data_if_ = nullptr;
    ESP_ERROR_CHECK(i2s_del_channel(tx_handle_));
    ESP_ERROR_CHECK(i2s_del_channel(rx_handle_));
    tx_handle_ = rx_handle_ = nullptr;
    callback_refill_ = false;
    CreateDuplexChannels(duplex_pins_[0], duplex_pins_[1], duplex_pins_[2],
                         duplex_pins_[3], duplex_pins_[4]);
    audio_codec_i2s_cfg_t config{};
    config.port = I2S_NUM_0;
    config.rx_handle = rx_handle_;
    config.tx_handle = tx_handle_;
    data_if_ = audio_codec_new_i2s_data(&config);
    assert(data_if_ != nullptr);
}

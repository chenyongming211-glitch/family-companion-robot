#include "software_aec_codec.h"
#if CONFIG_STOPWATCH_SOFTWARE_AEC
#include <esp_heap_caps.h>
#include <esp_timer.h>
#include <algorithm>
#include <cstdio>
#include <new>
#include "aec_diagnostics.h"
#include "application.h"
#if CONFIG_I2S_ISR_IRAM_SAFE
#error "Software AEC callback graph is not IRAM safe; disable I2S_ISR_IRAM_SAFE"
#endif
using D = family_robot::DuplexReference;

SoftwareAecCodec::SoftwareAecCodec(void* bus, i2c_port_t port, int input_rate, int output_rate,
                                   gpio_num_t mclk, gpio_num_t bclk, gpio_num_t ws, gpio_num_t dout,
                                   gpio_num_t din, gpio_num_t pa, uint8_t addr, bool use_mclk,
                                   bool inverted)
    : Es8311AudioCodec(bus, port, input_rate, output_rate, mclk, bclk, ws, dout, din, pa, addr,
                       use_mclk, inverted) {
    input_channels_ = 2;
    input_reference_ = true;  // ONLY logical format; esp_codec_dev_open stays mono.
    auto* storage = heap_caps_malloc(sizeof(D), MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (storage)
        pair_ = new (storage) D;
    const auto tx_disabled = i2s_channel_disable(tx_handle_);
    const auto rx_disabled = i2s_channel_disable(rx_handle_);
    if (pair_ && tx_disabled == ESP_OK && rx_disabled == ESP_OK && input_rate == D::kRate &&
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
        Fail(D::Fault::Setup);
}

void SoftwareAecCodec::Start() {
    Es8311AudioCodec::Start();
    void* storage = heap_caps_malloc(sizeof(AecDiagnostics), MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (storage)
        diagnostics_ = new (storage) AecDiagnostics(*this);
    infrastructure_ok_ = diagnostics_ && diagnostics_->Start();
    if (!infrastructure_ok_)
        Fail(D::Fault::Setup);
    printf("aec_init callbacks=%d infrastructure=%d\n", callbacks_ok_, infrastructure_ok_);
    PrintStats();
}
SoftwareAecCodec::State SoftwareAecCodec::ReadState() const {
    portENTER_CRITICAL(&mux_);
    const State state{valid_,
                      faulted_,
                      epochs_.capture(),
                      epochs_.wake(),
                      epochs_.integrity(),
                      aec_ready_generation_};
    portEXIT_CRITICAL(&mux_);
    return state;
}
void SoftwareAecCodec::NotifyAecReady(bool ready, uint32_t generation) {
    portENTER_CRITICAL(&mux_);
    aec_ready_generation_ =
        ready && valid_ && !faulted_ && generation == epochs_.capture() ? generation : UINT32_MAX;
    portEXIT_CRITICAL(&mux_);
}
void SoftwareAecCodec::PublishLocked() {
    const bool fault = !pair_ || pair_->fault() != D::Fault::None;
    if (fault)
        valid_ = false;
    if (fault && !faulted_) {
        epochs_.Fault();
        faulted_ = true;
        ++faults_;
        if (pair_ && pair_->fault() == D::Fault::Overflow)
            ++overruns_;
    }
    valid_ = !fault && pair_->eligible();
}
void SoftwareAecCodec::Fail(D::Fault f) {
    portENTER_CRITICAL(&mux_);
    if (f == D::Fault::Setup)
        setup_failed_ = true;
    if (pair_)
        pair_->Fail(f);
    PublishLocked();
    portEXIT_CRITICAL(&mux_);
}
void SoftwareAecCodec::OnDeviceEpoch(bool opened) {
    portENTER_CRITICAL(&mux_);
    accepting_ = false;
    valid_ = false;
    epochs_.HardwareChange();
    if (opened && pair_ && callbacks_ok_ && infrastructure_ok_ && !setup_failed_) {
        pair_->Begin(++hardware_generation_, CONFIG_STOPWATCH_AEC_OFFSET_SAMPLES);
        pair_->Capture(capture_active_);
        faulted_ = false;
        fault_reported_ = false;
        discard_tx_ = discard_rx_ = 1;  // partial boundary descriptor cannot establish an epoch
        last_tx_us_ = last_rx_us_ = 0;
        epoch_started_us_ = esp_timer_get_time();
        accepting_ = true;
    }
    portEXIT_CRITICAL(&mux_);
    if (opened) {
        i2s_chan_info_t tx = {}, rx = {};
        if (i2s_channel_get_info(tx_handle_, &tx) != ESP_OK ||
            i2s_channel_get_info(rx_handle_, &rx) != ESP_OK ||
            tx.total_dma_buf_size != 6 * D::kBlock * sizeof(int16_t) ||
            rx.total_dma_buf_size != 6 * D::kBlock * sizeof(int16_t))
            Fail(D::Fault::Format);
        else {
            for (const auto* info : {&tx, &rx}) {
                const auto* cfg = static_cast<const i2s_std_config_t*>(info->mode_cfg);
                if (info->mode != I2S_COMM_MODE_STD || !info->is_enabled || !cfg ||
                    cfg->clk_cfg.sample_rate_hz != D::kRate ||
                    cfg->slot_cfg.data_bit_width != I2S_DATA_BIT_WIDTH_16BIT ||
                    cfg->slot_cfg.slot_mode != I2S_SLOT_MODE_MONO ||
                    cfg->slot_cfg.slot_mask != I2S_STD_SLOT_LEFT || info->bclk_hz != D::kRate * 32)
                    Fail(D::Fault::Format);
            }
        }
        printf("aec_format tx_dma_bytes=%lu rx_dma_bytes=%lu tx_bclk_hz=%lu rx_bclk_hz=%lu\n",
               (unsigned long)tx.total_dma_buf_size, (unsigned long)rx.total_dma_buf_size,
               (unsigned long)tx.bclk_hz, (unsigned long)rx.bclk_hz);
        PrintStats();
    }
}
void SoftwareAecCodec::EnableInput(bool enable) {
    std::lock_guard<std::mutex> lock(lifecycle_);
    Es8311AudioCodec::EnableInput(enable);
}
void SoftwareAecCodec::EnableOutput(bool enable) {
    std::lock_guard<std::mutex> lock(lifecycle_);
    Es8311AudioCodec::EnableOutput(enable);
}
void SoftwareAecCodec::OutputData(std::vector<int16_t>& data) {
    std::lock_guard<std::mutex> lock(lifecycle_);
    // Completed DMA callback alone supplies reference. Never append submitted PCM.
    if (Es8311AudioCodec::Write(data.data(), data.size()) != static_cast<int>(data.size()))
        Fail(D::Fault::Setup);
}
void SoftwareAecCodec::SetCaptureActive(bool active) {
    portENTER_CRITICAL(&mux_);
    if (capture_active_ != active) {
        capture_active_ = active;
        valid_ = false;
        // Suspending after WakeNet detection freezes a valid preroll snapshot.
        // Resumption, hardware transitions and faults invalidate that snapshot.
        if (active)
            epochs_.Resume();
        else
            epochs_.Pause();
        if (pair_)
            pair_->Capture(active);
    }
    portEXIT_CRITICAL(&mux_);
}
void SoftwareAecCodec::CaptureBoundary() {
    portENTER_CRITICAL(&mux_);
    // Keep physical capture/TX history and frozen wake preroll; quarantine only
    // raw backlog and in-flight capture results at observer-auto tail boundary.
    valid_ = false;
    epochs_.Boundary();
    if (pair_) {
        pair_->Capture(false);
        pair_->Capture(capture_active_);
    }
    portEXIT_CRITICAL(&mux_);
}
bool SoftwareAecCodec::Sent(i2s_chan_handle_t, i2s_event_data_t* e, void* arg) {
    return static_cast<SoftwareAecCodec*>(arg)->Complete(e, true);
}
bool SoftwareAecCodec::Received(i2s_chan_handle_t, i2s_event_data_t* e, void* arg) {
    return static_cast<SoftwareAecCodec*>(arg)->Complete(e, false);
}
bool SoftwareAecCodec::Complete(i2s_event_data_t* e, bool tx) {
    const int64_t stamp = esp_timer_get_time();
    portENTER_CRITICAL_ISR(&mux_);
    if (accepting_ && pair_) {
        auto& discard = tx ? discard_tx_ : discard_rx_;
        if (e->size != D::kBlock * sizeof(int16_t) || !e->dma_buf)
            pair_->Fail(D::Fault::Format);
        else if (discard)
            --discard;
        else if (tx) {
            pair_->Tx(pair_->tx_samples(), stamp, static_cast<int16_t*>(e->dma_buf), D::kBlock);
            last_tx_us_ = stamp;
        } else {
            pair_->Rx(pair_->rx_samples(), stamp, static_cast<int16_t*>(e->dma_buf), D::kBlock);
            last_rx_us_ = stamp;
        }
        PublishLocked();
    }
    portEXIT_CRITICAL_ISR(&mux_);
    return false;
}
bool SoftwareAecCodec::InputData(std::vector<int16_t>& data) {
    // The input service requests10ms AFE frames or60ms audio-test frames.
    // Consume both through the same integer timeline; never round individual DMA packets.
    constexpr size_t kMaxFrames = 1440;
    if (data.empty() || data.size() % 2 != 0 || data.size() / 2 > kMaxFrames) {
        Fail(D::Fault::Format);
        return false;
    }
    const size_t frames = data.size() / 2;
    const int64_t deadline = esp_timer_get_time() + D::kHoldbackUs + frames * 1000000 / D::kRate;
    size_t done = 0;
    while (!ReferenceFaulted() && done < frames) {
        portENTER_CRITICAL(&mux_);
        if (!capture_active_) {
            portEXIT_CRITICAL(&mux_);
            return false;
        }
        const size_t count = pair_ ? std::min(frames - done, pair_->front_remaining()) : 0;
        const auto result = pair_ ? pair_->Take(data.data() + done * 2, count, esp_timer_get_time())
                                  : D::Result::Fault;
        PublishLocked();
        portEXIT_CRITICAL(&mux_);
        if (result == D::Result::Ready) {
            done += count;
            continue;
        }
        if (result == D::Result::Fault || result == D::Result::Warmup)
            return false;
        if (esp_timer_get_time() >= deadline) {
            Fail(D::Fault::Timeout);
            return false;
        }
        vTaskDelay(1);
    }
    if (done == frames)
        return true;
    if (done != 0)
        return false;  // never join a matched prefix and mono fallback in one frame
    // Valid auto fallback uses original ADC read. R slot is format padding only;
    // AFE Feed reset handshake disables AEC, including WakeNet, before this is fed.
    std::array<int16_t, kMaxFrames> mono{};
    std::lock_guard<std::mutex> lock(lifecycle_);
    if (!input_enabled_ || Es8311AudioCodec::Read(mono.data(), frames) != static_cast<int>(frames))
        return false;
    for (size_t i = 0; i < frames; ++i) {
        data[2 * i] = mono[i];
        data[2 * i + 1] = 0;
    }
    return true;
}

void SoftwareAecCodec::ResetDiagnosticStream(uint8_t stream) {
    if (diagnostics_)
        diagnostics_->ResetStream(stream);
}
void SoftwareAecCodec::RecordDiagnostic(uint8_t stream, const int16_t* pcm, size_t frames,
                                        uint8_t channels, uint32_t generation) {
    if (diagnostics_) {
        const auto state = ReadState();
        diagnostics_->Record(stream, pcm, frames, channels, generation,
                             generation == state.capture && state.valid && !state.faulted);
    }
}
void SoftwareAecCodec::PollFault() {
    bool report = false;
    portENTER_CRITICAL(&mux_);
    const auto now = esp_timer_get_time();
    if (accepting_ && pair_ && now - epoch_started_us_ > D::kHoldbackUs &&
        (now - last_tx_us_ > D::kHoldbackUs || now - last_rx_us_ > D::kHoldbackUs))
        pair_->Fail(D::Fault::Timeout);
    PublishLocked();
    if (faulted_ && !fault_reported_) {
        fault_reported_ = true;
        report = true;
    }
    portEXIT_CRITICAL(&mux_);
    if (report) {
        // One aggregate snapshot per fault epoch; no audio, transcript or credentials.
        PrintStats();
        Application::GetInstance().Schedule([] { Application::GetInstance().SetAecMode(kAecOff); });
    }
}
void SoftwareAecCodec::PrintStats() {
    uint32_t faults = 0, overruns = 0;
    int fault = 0, offset = CONFIG_STOPWATCH_AEC_OFFSET_SAMPLES;
    int64_t tx = 0, rx = 0, phase = 0, paired = 0;
    size_t pending = 0;
    portENTER_CRITICAL(&mux_);
    if (pair_) {
        fault = int(pair_->fault());
        tx = pair_->tx_samples();
        rx = pair_->rx_samples();
        phase = pair_->phase_us();
        paired = pair_->paired_samples();
        pending = pair_->pending_blocks();
    } else
        fault = int(D::Fault::Setup);
    faults = faults_;
    overruns = overruns_;
    portEXIT_CRITICAL(&mux_);
    printf(
        "aec_stats physical=24000/mono16 logical=MR gen=%lu eligible=%d fault=%d tx=%lld rx=%lld "
        "paired=%lld rx_pending=%u phase_us=%lld offset_samples=%d faults=%lu rx_overruns=%lu "
        "cadence_tolerance_us=1500 reference_bytes=%u rx_blocks=16\n",
        (unsigned long)CaptureGeneration(), RealtimeEligible(), fault, (long long)tx, (long long)rx,
        (long long)paired, unsigned(pending), (long long)phase, offset, (unsigned long)faults,
        (unsigned long)overruns, unsigned(sizeof(family_robot::PlaybackReferenceBuffer)));
}
#endif

#pragma once
#include "sdkconfig.h"
#if CONFIG_STOPWATCH_SOFTWARE_AEC
#include <atomic>
#include "codecs/es8311_audio_codec.h"
#include "duplex_reference.h"
#include "experiment_gate.h"

class AecDiagnostics;
class SoftwareAecCodec final : public Es8311AudioCodec {
public:
    SoftwareAecCodec(void* bus, i2c_port_t port, int input_rate, int output_rate, gpio_num_t mclk,
                     gpio_num_t bclk, gpio_num_t ws, gpio_num_t dout, gpio_num_t din, gpio_num_t pa,
                     uint8_t addr, bool use_mclk = true, bool inverted = false);
    void Start() override;
    bool InputData(std::vector<int16_t>& data) override;
    void OutputData(std::vector<int16_t>& data) override;
    void EnableInput(bool enable) override;
    void EnableOutput(bool enable) override;
    void SetCaptureActive(bool active) override;
    bool SoftwareReference() const override { return true; }
    bool ReferenceValid() const override { return valid_.load(); }
    void ReportReferenceFailure() override { Fail(family_robot::DuplexReference::Fault::Setup); }
    bool RealtimeEligible() const override {
        return ReferenceValid() && aec_ready_generation_.load() == CaptureGeneration();
    }
    void NotifyAecReady(bool ready, uint32_t generation = 0) override {
        aec_ready_generation_ = ready ? generation : UINT32_MAX;
    }
    bool ReferenceFaulted() const override { return faulted_.load(); }
    uint32_t WakeAudioGeneration() const override { return wake_generation_.load(); }
    uint32_t CaptureGeneration() const override { return capture_generation_.load(); }
    void ResetDiagnosticStream(uint8_t stream) override;
    void RecordDiagnostic(uint8_t stream, const int16_t* pcm, size_t frames, uint8_t channels,
                          uint32_t generation = 0) override;
    void PrintStats();
    void PollFault();

protected:
    void OnDeviceEpoch(bool opened) override;

private:
    static bool Sent(i2s_chan_handle_t, i2s_event_data_t*, void*);
    static bool Received(i2s_chan_handle_t, i2s_event_data_t*, void*);
    bool Complete(i2s_event_data_t* event, bool tx);
    void Fail(family_robot::DuplexReference::Fault fault);
    void PublishLocked();
    mutable portMUX_TYPE mux_ = portMUX_INITIALIZER_UNLOCKED;
    std::mutex lifecycle_;
    family_robot::CaptureEpoch epochs_;
    void PublishEpochLocked() {
        capture_generation_ = epochs_.capture();
        wake_generation_ = epochs_.wake();
    }
    family_robot::DuplexReference* pair_ = nullptr;
    AecDiagnostics* diagnostics_ = nullptr;
    std::atomic<uint32_t> valid_{0}, faulted_{0};
    std::atomic<uint32_t> capture_generation_{0}, wake_generation_{0},
        aec_ready_generation_{UINT32_MAX};
    bool infrastructure_ok_ = false, setup_failed_ = false;
    bool callbacks_ok_ = false, accepting_ = false, capture_active_ = false;
    bool fault_reported_ = false;
    uint8_t discard_tx_ = 0, discard_rx_ = 0;
    uint32_t hardware_generation_ = 0;
    uint32_t faults_ = 0, overruns_ = 0;
    int64_t last_tx_us_ = 0, last_rx_us_ = 0, epoch_started_us_ = 0;
};
#endif

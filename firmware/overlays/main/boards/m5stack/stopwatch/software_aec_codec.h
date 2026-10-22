#pragma once
#include "sdkconfig.h"
#if CONFIG_STOPWATCH_SOFTWARE_AEC
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
    bool PlaybackSupported() const override { return playback_ != nullptr && callbacks_ok_; }
    bool PlaybackControl(PlaybackOp op, family_robot::PlaybackIdentity id = {}, uint64_t first = 0, size_t n = 0) override;
    uint32_t PlaybackGeneration() const override;
    bool PlaybackPoll(family_robot::PlaybackSnapshot& out) override;
    bool PlaybackBusy() const override;
    void OutputTagged(std::vector<int16_t>& pcm, family_robot::PlaybackIdentity id,
                      uint64_t first, uint32_t generation) override;
    void EnableInput(bool enable) override;
    void EnableOutput(bool enable) override;
    void SetCaptureActive(bool active) override;
    void CaptureBoundary() override;
    uint32_t ReferenceIntegrity() const override { return ReadState().integrity; }
    bool SoftwareReference() const override { return true; }
    bool ReferenceValid() const override { return ReadState().valid; }
    void ReportReferenceFailure() override { Fail(family_robot::DuplexReference::Fault::Setup); }
    bool RealtimeEligible() const override {
        const auto state = ReadState();
        return state.valid && !state.faulted && state.aec_ready == state.capture;
    }
    void NotifyAecReady(bool ready, uint32_t generation = 0) override;
    bool ReferenceFaulted() const override { return ReadState().faulted; }
    uint32_t WakeAudioGeneration() const override { return ReadState().wake; }
    uint32_t CaptureGeneration() const override { return ReadState().capture; }
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
    // Requires mux_ already held, whether called from task or ISR. Never calls
    // task-facing snapshot/getter methods, takes another lock or uses std::atomic.
    void PublishLocked();
    struct State {
        bool valid, faulted;
        uint32_t capture, wake, integrity, aec_ready;
    };
    // Task-only, acquire mux_ exactly once and return a value snapshot.
    State ReadState() const;
    mutable portMUX_TYPE mux_ = portMUX_INITIALIZER_UNLOCKED;
    std::mutex lifecycle_;
    family_robot::CaptureEpoch epochs_;
    family_robot::DuplexReference* pair_ = nullptr;
    AecDiagnostics* diagnostics_ = nullptr;
    family_robot::PlaybackProgress* playback_ = nullptr;
    uint32_t output_generation_ = 0;
    // ISR-shared status and epochs_ are plain internal-SRAM state under mux_.
    // Xtensa's std::atomic<uint32_t> is not guaranteed lock-free by this toolchain.
    bool valid_ = false, faulted_ = false;
    uint32_t aec_ready_generation_ = UINT32_MAX;
    bool infrastructure_ok_ = false, setup_failed_ = false;
    bool callbacks_ok_ = false, accepting_ = false, capture_active_ = false;
    bool fault_reported_ = false;
    uint8_t discard_tx_ = 0, discard_rx_ = 0;
    uint32_t hardware_generation_ = 0;
    uint32_t faults_ = 0, overruns_ = 0;
    int64_t last_tx_us_ = 0, last_rx_us_ = 0, epoch_started_us_ = 0;
};
#endif

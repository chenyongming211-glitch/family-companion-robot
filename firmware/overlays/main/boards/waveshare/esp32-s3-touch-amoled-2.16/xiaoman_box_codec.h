#pragma once
#include "sdkconfig.h"
#if CONFIG_WAVESHARE_XIAOMAN_REALTIME
#include "codecs/box_audio_codec.h"
#include "experiment_gate.h"

// Hardware MR remains ES7210 input, including the physical DAC reference.
// Only TX ownership changes: completed descriptors are refilled by the ISR.
class XiaomanBoxCodec final : public BoxAudioCodec {
public:
    XiaomanBoxCodec(void* bus, int input_rate, int output_rate, gpio_num_t mclk,
                    gpio_num_t bclk, gpio_num_t ws, gpio_num_t dout, gpio_num_t din,
                    gpio_num_t pa, uint8_t dac, uint8_t adc, bool reference);
    ~XiaomanBoxCodec() override;
    void Start() override;
    bool InputData(std::vector<int16_t>& data) override;
    void OutputData(std::vector<int16_t>& data) override;
    void EnableInput(bool enable) override;
    void EnableOutput(bool enable) override;
    bool PlaybackSupported() const override;
    bool PlaybackControl(PlaybackOp op, family_robot::PlaybackIdentity id={}, uint64_t first=0, size_t n=0) override;
    uint32_t PlaybackGeneration() const override;
    bool PlaybackPoll(family_robot::PlaybackSnapshot& out) override;
    bool PlaybackBusy() const override;
    void OutputTagged(std::vector<int16_t>& pcm, family_robot::PlaybackIdentity id,
                      uint64_t first, uint32_t generation) override;
#if CONFIG_XIAOMAN_ORB_UI
    family_robot::OrbOutputActivity GetOrbOutputActivity() const override;
    void MarkOrbDraining(family_robot::PlaybackIdentity id) override;
#endif
    bool ManagedCapture() const override { return true; }
    bool SoftwareReference() const override { return false; }
    bool ReferenceValid() const override;
    bool ReferenceFaulted() const override;
    bool RealtimeEligible() const override;
    void NotifyAecReady(bool ready, uint32_t generation=0) override;
    void ReportReferenceFailure() override;
    uint32_t CaptureGeneration() const override;
    uint32_t WakeAudioGeneration() const override;
    uint32_t ReferenceIntegrity() const override;
    void SetCaptureActive(bool active) override;
    void CaptureBoundary() override;
private:
    void InitializePlayback(int input_rate, int output_rate, bool reference);
    bool VerifyHardware(); // lifecycle_ held: mode_cfg points into mutable driver state
    bool PreparePlaybackHardware(); // lifecycle_ held, before Start creates its record
    void Disarm();
    void Arm();
    void FaultLocked(uint32_t source=5); // mux_ held, ISR-safe scalar state only
    static bool Sent(i2s_chan_handle_t, i2s_event_data_t*, void*);
    bool Complete(i2s_event_data_t* event);
    mutable portMUX_TYPE mux_=portMUX_INITIALIZER_UNLOCKED;
    std::mutex lifecycle_;
    family_robot::PlaybackProgress* playback_=nullptr;
    family_robot::CaptureEpoch epochs_;
    uint32_t output_generation_=0, aec_ready_=UINT32_MAX;
    bool callbacks_ok_=false, setup_failed_=false, accepting_=false;
    bool faulted_=false, capture_active_=false;
    uint32_t fault_source_=0;
    size_t last_dma_bytes_=0;
    int64_t last_dma_us_=0, last_dma_gap_us_=0;
    bool fault_logged_=false;
};
#endif

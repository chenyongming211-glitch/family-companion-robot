#pragma once
#include "sdkconfig.h"
#if CONFIG_XMINI_C3_BUTTON_ENTRY
#include "codecs/es8311_audio_codec.h"
#if CONFIG_XMINI_C3_VOICE_BARGE
#include "../../m5stack/stopwatch/duplex_reference.h"
#endif

// TX completion exclusively owns running DMA refills. The optional C3 barge
// path adds bounded mono RX / completed TX pairing without changing I2S shape.
class FamilyC3Codec final : public Es8311AudioCodec {
public:
    FamilyC3Codec(void* bus, i2c_port_t port, int input_rate, int output_rate,
                  gpio_num_t mclk, gpio_num_t bclk, gpio_num_t ws, gpio_num_t dout,
                  gpio_num_t din, gpio_num_t pa, uint8_t addr,
                  bool use_mclk=true, bool inverted=false);
    ~FamilyC3Codec() override;
    void Start() override;
    void EnableInput(bool enable) override;
    void EnableOutput(bool enable) override;
    bool InputData(std::vector<int16_t>& data) override;
    void CaptureBoundary() override;
    void OutputData(std::vector<int16_t>& data) override;
    bool PlaybackSupported() const override;
    bool PlaybackControl(PlaybackOp op, family_robot::PlaybackIdentity id={}, uint64_t first=0, size_t n=0) override;
    uint32_t PlaybackGeneration() const override;
    bool PlaybackPoll(family_robot::PlaybackSnapshot& out) override;
    bool PlaybackBusy() const override;
    void OutputTagged(std::vector<int16_t>& pcm, family_robot::PlaybackIdentity id,
                      uint64_t first, uint32_t generation) override;
    bool ReferenceFaulted() const override;
#if CONFIG_XMINI_C3_VOICE_BARGE
    void SetCaptureActive(bool active) override;
    uint32_t CaptureGeneration() const override;
    bool ReferenceValid() const override;
    void NotifyAecReady(bool ready, uint32_t generation=0) override;
    void ReportReferenceFailure() override;
    bool RealtimeEligible() const override;
#else
    bool RealtimeEligible() const override { return false; }
#endif
protected:
    void OnDeviceEpoch(bool opened) override;
    int Read(int16_t* dest, int samples) override;
    int Write(const int16_t* data, int samples) override;
private:
    void InitializePlayback(int input_rate, int output_rate);
    bool VerifyHardware(); // lifecycle_ held; mode_cfg belongs to mutable SDK state
    bool PrepareHardware();
    bool WaitForSilence(); // lifecycle_ held, never mux_ while waiting
    void ClearClosedDescriptors();
    void StopFaultedOutput(); // task-only PA mute; lifecycle try_lock never waits
    void DisarmLocked(); // mux_ held
    void FaultLocked(); // mux_ held, ISR-safe
    void AdvanceGenerationLocked();
    static bool Sent(i2s_chan_handle_t, i2s_event_data_t*, void*);
    bool Complete(i2s_event_data_t* event);
#if CONFIG_XMINI_C3_VOICE_BARGE
    using Pair = family_robot::BoundedDuplexReference<4096, 8, 75000>;
    static_assert(sizeof(Pair) <= 13000, "C3 reference storage must stay bounded");
    static bool Received(i2s_chan_handle_t, i2s_event_data_t*, void*);
    void ResetCaptureLocked(); // mux_ held; retain physical history and clocks
    void CheckReferenceDeadlineLocked(int64_t now); // mux_ held, task-only
    void PublishReferenceLocked(); // mux_ held; fail closed on pairing error
    bool InputPaired(std::vector<int16_t>& data);
    Pair* pair_=nullptr;
    uint32_t physical_generation_=0,capture_generation_=0,aec_ready_generation_=UINT32_MAX;
    bool capture_active_=false,capture_boundary_pending_=false,reference_valid_=false;
    bool reference_callbacks_armed_=false,rx_callback_registered_=false,tx_callback_registered_=false;
    unsigned discard_tx_=0,discard_rx_=0;
    int64_t reference_tx_us_=0,reference_rx_us_=0;
#endif
    mutable portMUX_TYPE mux_=portMUX_INITIALIZER_UNLOCKED;
    std::mutex lifecycle_;
    family_robot::PlaybackProgress* playback_=nullptr;
    uint32_t output_generation_=0,last_connection_=0;
    int64_t pending_since_us_=0,last_tx_us_=0;
    bool callbacks_ok_=false,setup_failed_=false,faulted_=false;
    bool hardware_open_=false,verified_=false,accepting_=false;
    bool fault_stop_done_=false;
    gpio_num_t fault_pa_pin_=GPIO_NUM_NC;
    bool fault_pa_inverted_=false;
    // Known addresses are only cleared by a task after both channels stop.
    // During a logical OUT mute the running callback scrubs a complete ring.
    int16_t* dma_addresses_[family_robot::PlaybackProgress::kSlots]{};
    int16_t* scrub_addresses_[family_robot::PlaybackProgress::kSlots]{};
    size_t known_descriptors_=0,scrubbed_=0;
    bool scrub_pending_=false;
};
#endif

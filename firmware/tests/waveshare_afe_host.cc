#include <atomic>
struct AfeCodec {
    XiaomanBoxCodec* real;
    bool software=false;
    bool input_reference() const {return true;}
    bool SoftwareReference() const {return software;}
    bool ManagedCapture() const {return true;}
    bool ReferenceValid() const {return real->ReferenceValid();}
    uint32_t WakeAudioGeneration() const {return real->WakeAudioGeneration();}
    void NotifyAecReady(bool ready,uint32_t capture=0) {real->NotifyAecReady(ready,capture);}
};
using EventBits_t=unsigned;
static EventBits_t xEventGroupGetBits(unsigned bits) {return bits;}
static bool aec_on=false;
static int enable_result=1,disable_result=0;
static void enable_wake(void*) {}
static void disable_wake(void*) {}
static int enable_aec(void*) {if(enable_result>=0)aec_on=enable_result==1;return enable_result;}
static int disable_aec(void*) {if(disable_result>=0)aec_on=disable_result==1;return disable_result;}
struct AfeIface {
    void (*enable_wakenet)(void*)=enable_wake;
    void (*disable_wakenet)(void*)=disable_wake;
    int (*enable_aec)(void*)=::enable_aec;
    int (*disable_aec)(void*)=::disable_aec;
};
class AfeAudioEngine {
public:
    enum class WakeDetector {kNone,kWakeNet,kMultiNet};
    static constexpr unsigned kWakeWordEnabled=1,kVoiceProcessingEnabled=2;
    AfeCodec* codec_=nullptr;
    void* afe_data_=nullptr;
    AfeIface iface;
    AfeIface* afe_iface_=&iface;
    unsigned event_group_=kVoiceProcessingEnabled;
    std::atomic<bool> device_aec_enabled_{false},applied_reference_valid_{false},applied_aec_enabled_{false};
    std::atomic<uint32_t> wake_batch_{1},wake_batch_generation_{0};
    WakeDetector wake_detector_=WakeDetector::kMultiNet;
    void ApplyAfeControls();
    void NotifyProcessedCaptureReady(uint32_t capture);
    // PRODUCTION_BATCH_GUARD
};
// PRODUCTION_METHODS
int main() {
    auto codec=make_codec();codec.Start();codec.SetCaptureActive(true);
    int16_t dma[240]{};tick(codec,dma);
    AfeCodec ac{&codec};AfeAudioEngine engine;engine.codec_=&ac;
    const auto capture=codec.CaptureGeneration();
    engine.ApplyAfeControls();
    assert(!aec_on && engine.applied_reference_valid_.load() && !engine.applied_aec_enabled_.load());
    engine.NotifyProcessedCaptureReady(capture);
    assert(!codec.RealtimeEligible()); // physical MR validity alone never proves applied AEC
    engine.device_aec_enabled_=true;engine.ApplyAfeControls();
    assert(aec_on && engine.applied_aec_enabled_.load() && !codec.RealtimeEligible());
    engine.NotifyProcessedCaptureReady(capture-1);assert(!codec.RealtimeEligible());
    engine.NotifyProcessedCaptureReady(capture);assert(codec.RealtimeEligible());
    for(int failure:{0,-1}) {
        enable_result=failure;engine.ApplyAfeControls();
        assert(!engine.applied_aec_enabled_.load() && !codec.RealtimeEligible());
        engine.NotifyProcessedCaptureReady(capture);assert(!codec.RealtimeEligible());
    }
    enable_result=1;engine.ApplyAfeControls();engine.NotifyProcessedCaptureReady(capture);
    assert(aec_on && codec.RealtimeEligible());
    disable_result=-1;engine.device_aec_enabled_=false;engine.ApplyAfeControls();
    engine.NotifyProcessedCaptureReady(capture);assert(aec_on && !codec.RealtimeEligible());
    disable_result=0;
    engine.device_aec_enabled_=false;engine.ApplyAfeControls();
    engine.NotifyProcessedCaptureReady(capture);assert(!aec_on && !codec.RealtimeEligible());
    engine.event_group_|=engine.kWakeWordEnabled;engine.ApplyAfeControls();
    engine.NotifyProcessedCaptureReady(capture);assert(aec_on && codec.RealtimeEligible());
    engine.event_group_=engine.kVoiceProcessingEnabled;ac.software=true;
    engine.ApplyAfeControls();engine.NotifyProcessedCaptureReady(capture);
    assert(aec_on && codec.RealtimeEligible()); // original software observation policy stays enabled
    ac.software=false;
    engine.wake_batch_generation_=codec.WakeAudioGeneration();
    assert(engine.WakeAudioBatchValid());
    codec.PlaybackControl(AudioCodec::PlaybackOp::Connect,{1,0});
    assert(engine.WakeAudioBatchValid() && !codec.RealtimeEligible());
    codec.EnableOutput(false);codec.EnableOutput(true);
    assert(!engine.WakeAudioBatchValid()); // also protects Pop-to-Send and final EOF checks
    // Normal AudioService power idle on actual S3 HW2: the input task closes
    // IN first, then the output timer closes OUT. ReadAudioData later calls
    // only EnableInput(true), so that override must prepare the complete pair.
    engine.device_aec_enabled_=true; // the dedicated target's requested realtime mode
    codec.SetCaptureActive(false);codec.EnableInput(false);
    assert(!codec.rx.enabled && codec.tx.enabled && !codec.ReferenceFaulted());
    codec.EnableOutput(false);
    assert(!codec.rx.enabled && !codec.tx.enabled && codec.PlaybackSupported());
    const auto before_open=codec.PlaybackGeneration();
    const auto before_calls=codec.input_changes+codec.output_changes;
    codec.PlaybackControl(AudioCodec::PlaybackOp::Connect,{2,0});
    assert(codec.input_changes+codec.output_changes==before_calls);
    codec.EnableInput(true);codec.SetCaptureActive(true);
    assert(codec.input_enabled_ && codec.output_enabled_ && codec.rx.enabled && codec.tx.enabled);
    assert(codec.ReferenceValid() && !codec.RealtimeEligible() && !codec.ReferenceFaulted());
    assert(codec.input_changes+codec.output_changes==before_calls+2);
    assert(codec.PlaybackGeneration()==before_open+2); // Connect and one physical transition
    std::vector<int16_t> input(480);assert(codec.InputData(input));
    engine.ApplyAfeControls();
    assert(engine.applied_aec_enabled_.load() && !codec.RealtimeEligible());
    engine.NotifyProcessedCaptureReady(capture);assert(!codec.RealtimeEligible());
    engine.NotifyProcessedCaptureReady(codec.CaptureGeneration());assert(codec.RealtimeEligible());
    const auto prepared=codec.PlaybackGeneration();
    const auto prepared_calls=codec.input_changes+codec.output_changes;
    codec.EnableInput(true);codec.EnableOutput(true);
    assert(codec.PlaybackGeneration()==prepared && codec.input_changes+codec.output_changes==prepared_calls);
    // Interleaved OUT-first close uses the real pending-TX branch. Both
    // close orders fully stop the physical pair and subsequent IN restart
    // restores reference eligibility only after a fresh applied-AEC frame.
    codec.EnableOutput(false);assert(codec.tx.enabled && codec.rx.enabled);
    codec.EnableInput(false);assert(!codec.tx.enabled && !codec.rx.enabled && !codec.ReferenceFaulted());
    codec.EnableInput(true);assert(codec.ReferenceValid() && codec.output_enabled_);
    engine.ApplyAfeControls();engine.NotifyProcessedCaptureReady(codec.CaptureGeneration());
    assert(codec.RealtimeEligible());
    codec.EnableInput(false);codec.EnableOutput(false);
    assert(!codec.tx.enabled && !codec.rx.enabled && !codec.ReferenceFaulted());
    codec.EnableInput(true);assert(codec.ReferenceValid() && !codec.RealtimeEligible());
    codec.ReportReferenceFailure();engine.ApplyAfeControls();
    engine.NotifyProcessedCaptureReady(codec.CaptureGeneration());
    assert(!engine.applied_reference_valid_.load() && !aec_on && !codec.RealtimeEligible());
    codec.EnableInput(false);codec.EnableOutput(false);
    const auto faulted_output_calls=codec.output_changes;
    codec.EnableInput(true);
    assert(codec.ReferenceFaulted() && !codec.ReferenceValid() && !codec.PlaybackSupported());
    assert(!codec.output_enabled_ && codec.output_changes==faulted_output_calls);
    assert(!codec.InputData(input)); // normal pair completion never clears a latched fault
    puts("production AFE applied-AEC readiness and public hardware wake-batch guard passed");
}

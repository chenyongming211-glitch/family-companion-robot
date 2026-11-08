#include "orb_activity.h"
#include "playback_progress.h"
#include "experiment_gate.h"
#include "face_refresh_policy.h"
#include <array>
#include <atomic>
#include <cassert>
#include <cstdio>
#include <future>
#include <mutex>
#include <thread>
using namespace family_robot;
class Codec {
public:
    uint32_t capture=4;bool fault=false,software=true,hardware=false,eligible=false;
    family_robot::OrbOutputActivity output{};
    uint32_t CaptureGeneration(){return capture;}
    bool SoftwareReference(){return software;}
    bool ManagedCapture(){return software || hardware;}
    bool RealtimeEligible(){return eligible && !fault;}
    bool ReferenceFaulted(){return fault;}
    uint32_t ReferenceIntegrity(){return 7;}
    family_robot::OrbOutputActivity GetOrbOutputActivity(){return output;}
};
class AudioService {
public:
    std::mutex audio_queue_mutex_;Codec* codec_=nullptr;family_robot::ExperimentGate experiment_gate_;
    family_robot::UplinkAuthorization experiment_authorization_;
    std::atomic<uint32_t> capture_generation_{4};family_robot::OrbInputActivity orb_input_activity_{};
    std::atomic<uint32_t> orb_gate_epoch_{0};
    bool processor=false;bool IsAudioProcessorRunning(){return processor;}
    family_robot::OrbAudioActivity GetOrbAudioActivity();
};
#include "orb_audio_service.inc"
template<class T> static bool SnapshotAvailable(const T& audio) {
    if constexpr(requires {audio.snapshot_available;}) return audio.snapshot_available;
    return true;
}
static void DmaTruth() {
    std::array<int16_t,240> pcm{};
    assert(OrbPcmLevel(pcm.data(),pcm.size())==0);
    pcm.fill(32767); assert(OrbPcmLevel(pcm.data(),pcm.size())==255);
    pcm.fill(-32768); assert(OrbPcmLevel(pcm.data(),pcm.size())==255);
    pcm.fill(2000); const auto level=OrbPcmLevel(pcm.data(),pcm.size());
    assert(level>0 && level<255);
    PlaybackProgress p;
    const PlaybackIdentity id{7,1}; p.Connect(7); assert(p.Start(id));
    assert(p.Admit(id,0,240));
    assert(p.Submit(id,0,pcm.data(),240,level)==PlaybackProgress::SubmitResult::Accepted);
    assert(p.OrbActivity().stamp_us==0); // enqueue is not playback
    std::array<std::array<int16_t,240>,6> dma{};
    int64_t now=1000000;
    for (auto& block:dma) {
        p.Complete(block.data(),240,now); now+=10000;
        assert(p.OrbActivity().stamp_us==0); // discovery/refill isn't completion
    }
    p.Complete(dma[0].data(),240,now);
    auto a=p.OrbActivity();
    assert(a.valid && a.identity==id && a.level==level && a.stamp_us==now);
    const auto first_dma=now;
    assert(a.playbacks[0].first_us==first_dma && a.playbacks[0].last_us==first_dma);
    // Once sealed and physically drained it must not keep claiming speech.
    p.Release(id); p.Seal(id,false);
    assert(!p.OrbActivity().active);
    PlaybackSnapshot final;
    assert(p.NextReport(now+1000000,final) && final.finished);
    a=p.OrbActivity();
    assert(a.valid && a.playbacks[0].terminal && a.playbacks[0].last_us==first_dma);
    assert(!p.Snapshot(id).valid); // observation tombstone is not a receipt record
    p.HardwareFault(); assert(!p.OrbActivity().valid);
    p.Connect(8); assert(p.OrbActivity().stamp_us==0);
    assert(p.Start({8,1}));
    // Old DMA completions cannot animate a new connection.
    now+=10000; p.Complete(dma[1].data(),240,now);
    assert(p.OrbActivity().stamp_us==0);

    PlaybackProgress drain; drain.Connect(9); const PlaybackIdentity old{9,1};
    assert(drain.Start(old)); assert(drain.Admit(old,0,240));
    assert(drain.Submit(old,0,pcm.data(),240,level)==PlaybackProgress::SubmitResult::Accepted);
    drain.Complete(dma[0].data(),240,1000000);
    drain.MarkOrbDraining(old);
    drain.Release(old);
    drain.Seal(old,false); // negotiated drain preserves queued PCM and physical tail
    assert(drain.OrbActivity().draining); // real submitted tail only
    for (size_t i=1;i<6;++i) drain.Complete(dma[i].data(),240,1000000+int64_t(i)*10000);
    drain.Complete(dma[0].data(),240,1060000);
    assert(!drain.OrbActivity().draining);
}

static void RefreshPolicyTruth() {
    FaceRefreshPolicy policy;
    assert(policy.requested()==2 && policy.effective()==2);
    // Reproduce the observed cold/preemption spike without changing user intent.
    policy.ObserveFrame(138822);
    assert(policy.requested()==2 && policy.effective()==1);
    for(int i=0;i<9;++i) {policy.ObserveFrame(25000);assert(policy.effective()==1);}
    policy.ObserveFrame(25000);assert(policy.effective()==2);
    // Recovery needs ten consecutive fast frames, not an accumulating total.
    policy.ObserveFrame(30000);assert(policy.effective()==1);
    for(int i=0;i<6;++i) policy.ObserveFrame(10000);
    policy.ObserveFrame(25001);
    for(int i=0;i<9;++i) {policy.ObserveFrame(10000);assert(policy.effective()==1);}
    policy.ObserveFrame(10000);assert(policy.effective()==2);
    policy.Request(1);
    for(int i=0;i<30;++i) policy.ObserveFrame(5000);
    assert(policy.requested()==1 && policy.effective()==1);
    policy.Request(0);
    policy.ObserveFrame(200000);
    for(int i=0;i<30;++i) policy.ObserveFrame(5000);
    assert(policy.requested()==0 && policy.effective()==0);
    policy.Request(2);
    policy.ObserveFrame(90000);assert(policy.effective()==1);
    policy.ObserveFrame(90000);assert(policy.effective()==0 && policy.requested()==2);
    // A critical static fallback does not generate frames to probe recovery.
    for(int i=0;i<30;++i) policy.ObserveFrame(5000);
    assert(policy.effective()==0);
    policy.Request(2); // same requested value is an explicit retry
    assert(policy.requested()==2 && policy.effective()==2);
    policy.ObserveFrame(90000);
    policy.ObserveFrame(30000); // isolated critical frames cannot latch static
    policy.ObserveFrame(90000);assert(policy.effective()==1);
    assert(sizeof(FaceRefreshPolicy)<=16);
}


int main(){DmaTruth();RefreshPolicyTruth();
    Codec codec;AudioService service;service.codec_=&codec;service.processor=true;
    service.experiment_authorization_.Request(true,7);service.experiment_gate_.Set(true,4);
    assert(service.GetOrbAudioActivity().gate_open);
    service.capture_generation_=3;assert(!service.GetOrbAudioActivity().gate_open);
    service.capture_generation_=4;service.processor=false;assert(!service.GetOrbAudioActivity().gate_open);
    service.processor=true;service.experiment_gate_.Set(false,4);assert(!service.GetOrbAudioActivity().gate_open);
    service.processor=true;service.experiment_gate_.Set(true,4);
    service.orb_gate_epoch_=service.experiment_gate_.epoch();
    codec.output.connection=7;codec.output.identity={7,2};codec.output.valid=true;
    std::promise<void> held,release;auto released=release.get_future();
    std::thread holder([&]{std::lock_guard<std::mutex> lock(service.audio_queue_mutex_);held.set_value();released.wait();});
    held.get_future().wait();
    const auto unavailable=service.GetOrbAudioActivity();
    assert(!SnapshotAvailable(unavailable));
    assert(unavailable.capture==4 && unavailable.epoch==service.orb_gate_epoch_.load());
    assert(unavailable.output.connection==7); // current playback is read independently
    release.set_value();holder.join();
    assert(SnapshotAvailable(service.GetOrbAudioActivity()));
    // Hardware MR's actual processed-capture readiness is an extra prerequisite
    // for Listening. It never pretends to be a software reference.
    codec.software=false;codec.hardware=true;
    assert(!service.GetOrbAudioActivity().gate_open);
    codec.eligible=true;assert(service.GetOrbAudioActivity().gate_open);
    ++codec.capture;codec.eligible=false;
    assert(!service.GetOrbAudioActivity().gate_open);
std::puts("actual DMA and nonblocking UI activity passed");}

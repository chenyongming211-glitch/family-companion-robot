#include "playback_progress.h"
#include "audio_fault_storage.h"
#include <cstring>
#include <type_traits>
#include <array>
#include <cassert>
#include <cstdio>
using family_robot::PlaybackProgress;
using Reason = PlaybackProgress::FaultReason;
static_assert(static_cast<unsigned>(Reason::None) == 0);
static_assert(static_cast<unsigned>(Reason::External) == 1);
static_assert(static_cast<unsigned>(Reason::InvalidBuffer) == 2);
static_assert(static_cast<unsigned>(Reason::SampleRate) == 3);
static_assert(static_cast<unsigned>(Reason::Cadence) == 4);
static_assert(static_cast<unsigned>(Reason::DescriptorDuplicate) == 5);
static_assert(static_cast<unsigned>(Reason::DescriptorOrder) == 6);
static_assert(static_cast<unsigned>(Reason::DescriptorMissing) == 7);

void TestReasons() {
    PlaybackProgress progress;
    std::array<std::array<int16_t, PlaybackProgress::kBlock>, 7> dma{};
    assert(progress.HardwareFaultReason() == Reason::None);
    progress.HardwareFault();
    assert(progress.HardwareFaultReason() == Reason::External);
    progress.HardwareFault(Reason::Cadence);
    progress.Connect(42);
    progress.Complete(nullptr, PlaybackProgress::kBlock, 10000);
    assert(progress.HardwareFaultReason() == Reason::External);
    progress.HardwareReset();
    assert(progress.HardwareFaultReason() == Reason::None);
    assert(progress.HardwareHealthy());
    progress.Complete(nullptr, PlaybackProgress::kBlock, 10000);
    assert(progress.HardwareFaultReason() == Reason::InvalidBuffer);
    progress.HardwareReset();
    progress.Complete(dma[0].data(), PlaybackProgress::kBlock - 1, 10000);
    assert(progress.HardwareFaultReason() == Reason::InvalidBuffer);
    progress.HardwareReset();
    progress.Complete(dma[0].data(), PlaybackProgress::kBlock, 0);
    assert(progress.HardwareFaultReason() == Reason::InvalidBuffer);
    progress.HardwareReset(16000);
    progress.Complete(dma[0].data(), PlaybackProgress::kBlock, 10000);
    assert(progress.HardwareFaultReason() == Reason::SampleRate);
    progress.HardwareReset();
    progress.Complete(dma[0].data(), PlaybackProgress::kBlock, 10000);
    progress.Complete(dma[1].data(), PlaybackProgress::kBlock, 18499);
    assert(progress.HardwareFaultReason() == Reason::Cadence);
    progress.HardwareReset();
    progress.Complete(dma[0].data(), PlaybackProgress::kBlock, 10000);
    progress.Complete(dma[1].data(), PlaybackProgress::kBlock, 21501);
    assert(progress.HardwareFaultReason() == Reason::Cadence);
    progress.HardwareReset();
    progress.Complete(dma[0].data(), PlaybackProgress::kBlock, 10000);
    progress.Complete(dma[0].data(), PlaybackProgress::kBlock, 20000);
    assert(progress.HardwareFaultReason() == Reason::DescriptorDuplicate);
    for (auto reason : {Reason::DescriptorOrder, Reason::DescriptorMissing}) {
        progress.HardwareReset();
        for (size_t i = 0; i < PlaybackProgress::kSlots; ++i)
            progress.Complete(dma[i].data(), PlaybackProgress::kBlock, (i + 1) * 10000);
        assert(progress.HardwareHealthy());
        progress.Complete(dma[reason == Reason::DescriptorOrder ? 1 : 6].data(),
                          PlaybackProgress::kBlock, 70000);
        // Unknown addresses are distinguished from known descriptors out of order.
        assert(progress.HardwareFaultReason() == reason);
        progress.HardwareFault(Reason::External);
        assert(progress.HardwareFaultReason() == reason);
    }
    for (uint32_t rate : {24000u, 48000u}) {
        const int64_t period = rate == 24000 ? 10000 : 5000;
        progress.HardwareReset(rate);
        int64_t stamp = period;
        for (size_t i = 0; i < 18; ++i) {
            progress.Complete(dma[i % 6].data(), PlaybackProgress::kBlock, stamp);
            // Existing inclusive 85%-115% boundary remains accepted.
            stamp += (i % 2 ? period * 85 / 100 : period * 115 / 100);
        }
        assert(progress.HardwareHealthy());
        assert(progress.HardwareFaultReason() == Reason::None);
    }
}
using family_robot::AudioFaultSnapshot;
using family_robot::AudioFaultSource;
using family_robot::ValidateAudioFaultSnapshot;
using family_robot::LoadAudioFaultSnapshot;
using family_robot::SaveAudioFaultSnapshot;
static_assert(sizeof(AudioFaultSnapshot) == 40);
static_assert(std::is_trivially_copyable<AudioFaultSnapshot>::value);
static_assert(std::is_standard_layout<AudioFaultSnapshot>::value);
static_assert(static_cast<uint32_t>(AudioFaultSource::None) == 0);
static_assert(static_cast<uint32_t>(AudioFaultSource::HardwareConfiguration) == 1);
static_assert(static_cast<uint32_t>(AudioFaultSource::DmaShape) == 2);
static_assert(static_cast<uint32_t>(AudioFaultSource::DmaProgress) == 3);
static_assert(static_cast<uint32_t>(AudioFaultSource::Initialization) == 4);
static_assert(static_cast<uint32_t>(AudioFaultSource::InputRead) == 5);
static_assert(static_cast<uint32_t>(AudioFaultSource::MusicStart) == 6);
static_assert(static_cast<uint32_t>(AudioFaultSource::MusicRestore) == 7);
static_assert(static_cast<uint32_t>(AudioFaultSource::Recovery) == 8);
namespace {
AudioFaultSnapshot stored{};
size_t stored_size = sizeof(stored);
esp_err_t open_error=0, get_error=0, set_error=0, commit_error=0;
unsigned opens=0, gets=0, sets=0, commits=0, closes=0;
nvs_open_mode_t last_mode=NVS_READONLY;
void ResetNvs() {
    stored={}; stored.source=1; stored_size=sizeof(stored);
    open_error=get_error=set_error=commit_error=0;
    opens=gets=sets=commits=closes=0;
}
void AssertOutputUnchanged(const AudioFaultSnapshot& out) {
    assert(out.source==8 && out.boot==0xabcdef12 && out.at_us==1234);
}
}
esp_err_t nvs_open(const char* name, nvs_open_mode_t mode, nvs_handle_t* handle) {
    assert(std::strcmp(name,"audiofault")==0); ++opens; last_mode=mode;
    if(open_error) return open_error;
    *handle=17; return ESP_OK;
}
esp_err_t nvs_get_blob(nvs_handle_t handle, const char* key, void* out, size_t* size) {
    assert(handle==17 && std::strcmp(key,"first")==0); ++gets;
    assert(out && *size==sizeof(AudioFaultSnapshot));
    if(get_error) return get_error;
    if(stored_size<=*size) std::memcpy(out,&stored,stored_size);
    *size=stored_size; return ESP_OK;
}
esp_err_t nvs_set_blob(nvs_handle_t handle, const char* key, const void* data, size_t size) {
    assert(handle==17 && std::strcmp(key,"first")==0); ++sets;
    assert(size==sizeof(AudioFaultSnapshot));
    if(set_error) return set_error;
    std::memcpy(&stored,data,size); stored_size=size; return ESP_OK;
}
esp_err_t nvs_commit(nvs_handle_t handle) {
    assert(handle==17); ++commits; return commit_error;
}
void nvs_close(nvs_handle_t handle) { assert(handle==17); ++closes; }
void TestValidation() {
    AudioFaultSnapshot value{};
    assert(value.version==1 && value.source==0 && value.detail==0 && value.boot==0);
    assert(value.at_us==0 && value.dma_bytes==0 && value.dma_gap_us==0 && value.native==0);
    assert(!ValidateAudioFaultSnapshot(value));
    for(uint32_t source=1; source<=8; ++source) {
        value.source=source; value.detail=source==3?1:0;
        assert(ValidateAudioFaultSnapshot(value));
        value.detail=source==3?7:255;
        assert(ValidateAudioFaultSnapshot(value));
        ++value.detail; assert(!ValidateAudioFaultSnapshot(value));
    }
    value={}; value.source=1;
    value.version=0; assert(!ValidateAudioFaultSnapshot(value));
    value.version=2; assert(!ValidateAudioFaultSnapshot(value)); value.version=1;
    value.source=9; assert(!ValidateAudioFaultSnapshot(value));
    value.source=UINT32_MAX; assert(!ValidateAudioFaultSnapshot(value)); value.source=3;
    value.detail=0; assert(!ValidateAudioFaultSnapshot(value)); value.detail=1;
    value.at_us=-1; assert(!ValidateAudioFaultSnapshot(value)); value.at_us=INT64_MAX;
    assert(ValidateAudioFaultSnapshot(value));
    value.dma_gap_us=-2; assert(!ValidateAudioFaultSnapshot(value));
    value.dma_gap_us=-1; assert(ValidateAudioFaultSnapshot(value));
    value.dma_gap_us=INT64_MAX; assert(ValidateAudioFaultSnapshot(value));
    value.dma_bytes=65536; assert(ValidateAudioFaultSnapshot(value));
    value.dma_bytes=65537; assert(!ValidateAudioFaultSnapshot(value)); value.dma_bytes=0;
    value.native=INT32_MIN; value.boot=UINT32_MAX; assert(ValidateAudioFaultSnapshot(value));
}
void TestStorage() {
    ResetNvs();
    AudioFaultSnapshot value{};
    value.source=3; value.detail=5; value.boot=34; value.at_us=456789;
    value.dma_bytes=480; value.dma_gap_us=10000; value.native=-123;
    assert(SaveAudioFaultSnapshot(value));
    assert(opens==1 && sets==1 && commits==1 && closes==1 && last_mode==NVS_READWRITE);
    AudioFaultSnapshot output{};
    assert(LoadAudioFaultSnapshot(output));
    assert(opens==2 && gets==1 && closes==2 && last_mode==NVS_READONLY);
    assert(output.version==1 && output.source==3 && output.detail==5 && output.boot==34);
    assert(output.at_us==456789 && output.dma_bytes==480 && output.dma_gap_us==10000 && output.native==-123);
    // Helpers can save repeatedly; per-boot first-write eligibility belongs to the adapter.
    value.source=8; assert(SaveAudioFaultSnapshot(value)); assert(commits==2);
    for(int operation=0; operation<3; ++operation) {
        ResetNvs();
        if(operation==0) open_error=-1;
        if(operation==1) set_error=-2;
        if(operation==2) commit_error=-3;
        assert(!SaveAudioFaultSnapshot(value));
        assert(opens==1 && closes==(operation?1u:0u));
        assert(sets==(operation?1u:0u) && commits==(operation==2?1u:0u));
    }
    for(int failure=0; failure<7; ++failure) {
        ResetNvs(); output={}; output.source=8; output.boot=0xabcdef12; output.at_us=1234;
        if(failure==0) open_error=-1;
        if(failure==1) get_error=-2;
        if(failure==2) stored_size=sizeof(stored)-1;
        if(failure==3) stored_size=sizeof(stored)+1;
        if(failure==4) stored.version=2;
        if(failure==5) stored.source=9;
        if(failure==6) { stored.source=3; stored.detail=0; }
        assert(!LoadAudioFaultSnapshot(output));
        AssertOutputUnchanged(output);
        assert(opens==1 && gets==(failure?1u:0u) && closes==(failure?1u:0u));
        assert(sets==0 && commits==0);
    }
    ResetNvs(); value.version=2;
    assert(!SaveAudioFaultSnapshot(value));
    assert(opens==0 && closes==0 && sets==0 && commits==0);
}
int main() {
    TestReasons(); TestValidation(); TestStorage();
    std::puts("audio fault reason/storage tests passed");
}

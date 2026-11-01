#pragma once
#include "c3_pcm_buffer.h"
#include "c3_pcm_cues.h"
#include "c3_startup_diagnostics.h"
#include <atomic>
namespace family_robot {
// All members other than cue_generation/fault_epoch are under audio_queue_mutex_.
struct C3PcmAudioState {
    C3PcmInputPool input;
    C3PcmOutputPool output;
    C3PcmCuePack pack;
    struct Cue {C3PcmCuePack::Descriptor descriptor{};uint32_t frame=0,generation=0,output_generation=0;};
    static constexpr size_t kCues=32;
    Cue cues[kCues]{};
    size_t cue_head=0,cue_count=0;
    alignas(16) int16_t cue_scratch[C3PcmOutputPool::kSamples]{};
    std::atomic<uint32_t> cue_generation{1},fault_epoch{0};
    std::atomic<bool> fault_pending{false};
    bool receiving=false,cue_in_flight=false,cue_hardware=false;
    uint32_t input_faults=0,output_faults=0,media_faults=0,reference_faults=0;
};
static_assert(C3PcmInputPool::kSlots*C3PcmInputPool::kBytes+
              C3PcmOutputPool::kSlots*C3PcmOutputPool::kBytes+2880==21120,"fixed application PCM bytes");
}

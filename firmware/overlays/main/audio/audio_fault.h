#pragma once
#include <cstdint>

namespace family_robot {
// Persist numeric metadata only. Codes retain the existing hardware log sources.
enum class AudioFaultSource : uint32_t {
    None=0, HardwareConfiguration=1, DmaShape=2, DmaProgress=3,
    Initialization=4, InputRead=5, MusicStart=6, MusicRestore=7, Recovery=8
};
struct AudioFaultSnapshot {
    uint32_t version=1, source=0, detail=0, boot=0;
    int64_t at_us=0;
    int64_t dma_gap_us=0;
    uint32_t dma_bytes=0;
    int32_t native=0;
};
static_assert(sizeof(AudioFaultSnapshot)==40,"fixed audio fault NVS record without padding");
inline bool ValidateAudioFaultSnapshot(const AudioFaultSnapshot& snapshot) {
    if(snapshot.version!=1 || snapshot.source<1 || snapshot.source>8 ||
       snapshot.at_us<0 || snapshot.dma_bytes>65536 || snapshot.dma_gap_us < -1)
        return false;
    // DMA progress detail is PlaybackProgress::FaultReason, excluding None.
    if(snapshot.source==static_cast<uint32_t>(AudioFaultSource::DmaProgress))
        return snapshot.detail>=1 && snapshot.detail<=7;
    return snapshot.detail<=255;
}
} // namespace family_robot

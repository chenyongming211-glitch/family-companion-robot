#pragma once
#include <cstddef>
#include <cstdint>

namespace family_robot {
// Task-only mean absolute processed PCM amplitude. Saturating 8-bit logarithmic
// compression keeps quiet speech visible. No PCM is retained by the observer.
inline uint8_t OrbPcmLevel(const int16_t* pcm, size_t count) {
    if (!pcm || !count || count > 4096) return 0;
    uint32_t total=0;
    for (size_t i=0;i<count;++i) {
        const int32_t sample=pcm[i]; total+=sample<0?-sample:sample;
    }
    uint32_t mean=total/count;
    if (mean<32) return 0;
    // Piecewise compression without sqrt/log/float, bounded cost per block.
    uint32_t value=0;
    while (mean>255) { mean=(mean+1)/2; value+=32; }
    value+=mean/4;
    return static_cast<uint8_t>(value>255?255:value);
}
struct OrbInputActivity {
    uint32_t capture=0, epoch=0;
    uint8_t level=0;
    int64_t stamp_us=0;
};
// Separate from playback receipts: never used as evidence for server progress.
struct OrbOutputActivity {
    uint32_t connection=0;
    struct Identity {
        uint32_t connection=0,playback=0;
        template<class T> bool operator==(const T& other) const {
            return connection==other.connection && playback==other.playback;
        }
    } identity{};
    int64_t stamp_us=0;
    uint8_t level=0;
    bool active=false,draining=false,valid=false;
};
struct OrbAudioActivity {
    uint32_t capture=0,epoch=0;
    bool gate_open=false;
    OrbInputActivity input{};
    OrbOutputActivity output{};
};
} // namespace family_robot

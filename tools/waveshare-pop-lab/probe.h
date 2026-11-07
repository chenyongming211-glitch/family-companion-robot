#pragma once
#include <cstdint>
namespace wave_pop_lab {
// Called only under the codec's existing ISR mux. Idle and deliberate tail are
// excluded by the caller; this is a digital supply measurement, not a pop score.
struct Probe {
    uint32_t dma=0, empty=0, with_decode=0, with_pcm=0, without_backlog=0;
    uint32_t completed_samples=0;
    void Refill(bool measured, bool has_pcm, uint32_t decode, uint32_t pcm) {
        if (!measured) return;
        ++dma;
        if (has_pcm) return;
        ++empty;
        if (decode) ++with_decode;
        else if (pcm) ++with_pcm;
        else ++without_backlog;
    }
};
}

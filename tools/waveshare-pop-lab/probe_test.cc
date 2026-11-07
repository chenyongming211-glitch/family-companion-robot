#include "probe.h"
#include <cassert>

int main() {
    wave_pop_lab::Probe p;
    p.Refill(false, false, 3, 2);
    assert(p.dma == 0 && p.empty == 0);
    p.Refill(true, true, 3, 2);
    assert(p.dma == 1 && p.empty == 0);
    p.Refill(true, false, 3, 0);
    assert(p.empty == 1 && p.with_decode == 1 && p.with_pcm == 0);
    p.Refill(true, false, 0, 2);
    assert(p.empty == 2 && p.with_pcm == 1);
    p.Refill(true, false, 0, 0);
    assert(p.empty == 3 && p.without_backlog == 1);
    // An exhausted queue at the deliberate end is not an underrun measurement.
    p.Refill(false, false, 0, 0);
    assert(p.dma == 4 && p.empty == 3);
    p = {};
    assert(p.empty == 0 && p.dma == 0);
}

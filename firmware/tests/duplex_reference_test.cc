#include "duplex_reference.h"
#include <array>
#include <cassert>
#include <iostream>
using family_robot::DuplexReference;
using D = DuplexReference;
static std::array<int16_t, 240> ramp(int base) {
    std::array<int16_t, 240> a{};
    for (size_t i = 0; i < a.size(); ++i)
        a[i] = int16_t(base + int(i));
    return a;
}
int main() {
    D d;
    auto tx = ramp(11), mic = ramp(101);
    std::array<int16_t, 480> mr{};
    d.Begin(1, 0);
    d.Capture(true);
    assert(d.Tx(0, 10000, tx.data(), 240));
    assert(d.Rx(0, 10000, mic.data(), 240));
    assert(d.Take(mr.data(), 240, 11000) == D::Result::Ready);
    for (int i = 0; i < 240; ++i) {
        assert(mr[2 * i] == 101 + i);
        assert(mr[2 * i + 1] == 11 + i);
    }
    assert(d.eligible());
    // One physical output tail after interrupted decoder remains real reference.
    assert(d.Tx(240, 20000, tx.data(), 240));
    assert(d.Rx(240, 20000, mic.data(), 240));
    assert(d.Take(mr.data(), 100, 21000) == D::Result::Ready);
    assert(d.Take(mr.data(), 140, 21000) == D::Result::Ready);
    assert(mr[1] == 111);
    // Finite 24k sample timeline, wraps history many times, packet splits never drift.
    d.Begin(2, 0);
    d.Capture(true);
    for (int b = 0; b < 1200; ++b) {
        tx = ramp(b);
        assert(d.Tx(b * 240, (b + 1) * 10000, tx.data(), 240));
        assert(d.Rx(b * 240, (b + 1) * 10000, mic.data(), 240));
        assert(d.Take(mr.data(), 73, (b + 1) * 10000) == D::Result::Ready);
        for (int i = 0; i < 73; ++i)
            assert(mr[2 * i + 1] == b + i);
        assert(d.Take(mr.data(), 167, (b + 1) * 10000) == D::Result::Ready);
        for (int i = 0; i < 167; ++i)
            assert(mr[2 * i + 1] == b + 73 + i);
    }
    assert(d.tx_samples() == 288000);
    assert(d.rx_samples() == 288000);
    // Different callback phase: RX starts 125us (3 samples) after TX.
    d.Begin(3, 0);
    d.Capture(true);
    tx = ramp(11);
    assert(d.Tx(0, 10000, tx.data(), 240));
    assert(d.Rx(0, 10125, mic.data(), 240));
    assert(d.Take(mr.data(), 240, 10125) == D::Result::Wait);
    assert(d.Tx(240, 20000, tx.data(), 240));
    assert(d.Take(mr.data(), 240, 20125) == D::Result::Ready);
    assert(mr[1] == 14 && mr[2 * 236 + 1] == 250 && mr[2 * 237 + 1] == 11);
    // Known completed zero DMA is valid, unlike unavailable data.
    tx.fill(0);
    d.Begin(4, 0);
    d.Capture(true);
    assert(d.Tx(0, 10000, tx.data(), 240));
    assert(d.Rx(0, 10000, mic.data(), 240));
    assert(d.Take(mr.data(), 240, 10000) == D::Result::Ready && mr[1] == 0);
    assert(d.Rx(240, 20000, mic.data(), 240));
    assert(d.Take(mr.data(), 240, 20000) == D::Result::Wait);
    assert(d.Take(mr.data(), 240, 200001) == D::Result::Fault);
    assert(!d.eligible());
    d.Capture(false);
    d.Capture(true);
    assert(!d.eligible());  // fault latched
    // Restart is the only recovery; old samples cannot be returned.
    d.Begin(5, 0);
    d.Capture(true);
    assert(!d.eligible());
    assert(d.Tx(0, 10000, tx.data(), 240));
    assert(!d.Tx(480, 20000, tx.data(), 240));
    assert(d.fault() == D::Fault::Sequence);
    d.Begin(6, 0);
    d.Capture(true);
    assert(d.Rx(0, 10000, mic.data(), 240));
    assert(!d.Rx(0, 20000, mic.data(), 240));
    d.Begin(7, 0);
    d.Capture(true);
    for (int b = 0; b < 16; ++b)
        assert(d.Rx(b * 240, (b + 1) * 10000, mic.data(), 240));
    assert(!d.Rx(3840, 170000, mic.data(), 240));
    assert(d.fault() == D::Fault::Overflow);
    // Boot/output-only and intentional input pause do not arm an expected consumer.
    d.Begin(8, 0);
    for (int b = 0; b < 100; ++b) {
        assert(d.Tx(b * 240, (b + 1) * 10000, tx.data(), 240));
        assert(d.Rx(b * 240, (b + 1) * 10000, mic.data(), 240));
    }
    assert(d.fault() == D::Fault::None);
    d.Capture(true);
    assert(d.Rx(24000, 1010000, mic.data(), 240));
    assert(d.Tx(24000, 1010000, tx.data(), 240));
    assert(d.Take(mr.data(), 240, 1010000) == D::Result::Ready);
    d.Capture(false);
    assert(!d.eligible());
    assert(d.Tx(24240, 1020000, tx.data(), 240));
    assert(d.Rx(24240, 1020000, mic.data(), 240));
    d.Capture(true);
    assert(d.Take(mr.data(), 240, 1020000) == D::Result::Wait);
    assert(d.Tx(24480, 1030000, tx.data(), 240));
    assert(d.Rx(24480, 1030000, mic.data(), 240));
    assert(d.Take(mr.data(), 100, 1030000) == D::Result::Ready);
    d.Capture(false);
    d.Capture(true);  // explicitly discard an old partial frame on resume
    assert(d.Take(mr.data(), 240, 1030000) == D::Result::Wait);
    // Expired history and cadence gaps never masquerade as successful matching.
    d.Begin(9, 0);
    d.Capture(true);
    assert(d.Rx(0, 10000, mic.data(), 240));
    for (int b = 0; b < 70; ++b)
        assert(d.Tx(b * 240, (b + 1) * 10000, tx.data(), 240));
    assert(d.Take(mr.data(), 240, 700000) == D::Result::Fault);
    assert(d.fault() == D::Fault::Expired);
    d.Begin(10, 0);
    assert(d.Tx(0, 10000, tx.data(), 240));
    assert(!d.Tx(240, 30000, tx.data(), 240));
    // Fixed offset extremes: positive looks back; negative waits for future completion.
    for (int off : {-2400, 2400}) {
        d.Begin(11, off);
        d.Capture(true);
        for (int b = 0; b < 12; ++b) {
            assert(d.Tx(b * 240, (b + 1) * 10000, tx.data(), 240));
            assert(d.Rx(b * 240, (b + 1) * 10000, mic.data(), 240));
        }
        auto r = d.Take(mr.data(), 240, 120000);
        assert(r == (off > 0 ? D::Result::Warmup : D::Result::Ready));
    }
    d.Begin(12, 2401);
    assert(d.fault() == D::Fault::Format);
    d.Begin(13, 0);
    assert(!d.Tx(0, 10000, nullptr, 240));
    d.Begin(14, 0);
    assert(!d.Rx(0, 10000, mic.data(), 241));
    d.Begin(15, 0);
    assert(d.Take(nullptr, 240, 10000) == D::Result::Fault);
    d.Begin(16, 0);
    assert(!d.Tx(0, INT64_MAX, tx.data(), 240));
    std::cout << "pair_storage=" << sizeof(D)
              << " reference_storage=" << sizeof(family_robot::PlaybackReferenceBuffer) << "\n";
    std::cout << "duplex behavior PASS\n";
}

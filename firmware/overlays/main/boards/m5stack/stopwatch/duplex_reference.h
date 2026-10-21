#pragma once
#include <array>
#include <cstdlib>
#include "playback_reference_buffer.h"

namespace family_robot {
// All calls serialized by the adapter's single portMUX. No heap or platform APIs.
// 240 physical MONO16 frames = 10ms at 24k. Completion time anchors an epoch;
// subsequent mapping uses integer sample positions, never rounded packet durations.
class DuplexReference {
public:
    static constexpr size_t kBlock = 240, kRxBlocks = 16;
    static constexpr int kRate = 24000, kMaxOffset = 2400;
    static constexpr int64_t kHoldbackUs = 150000, kCadenceToleranceUs = 1500;
    static constexpr int64_t kMaxClockErrorPpm = 1000, kRateWindowUs = 10000000;
    enum class Fault { None, Format, Sequence, Cadence, Overflow, Expired, Timeout, Setup };
    enum class Result { Ready, Wait, Warmup, Fault };
    void Begin(uint32_t generation, int offset) {
        reference_.Reset(generation);
        generation_ = generation;
        offset_ = offset;
        fault_ = Fault::None;
        tx_ = {};
        rx_ = {};
        head_ = used_ = partial_ = 0;
        capture_ = ready_ = false;
        pairs_ = 0;
        if (offset < -kMaxOffset || offset > kMaxOffset)
            Fail(Fault::Format);
    }
    void Capture(bool enabled) {
        if (capture_ == enabled)
            return;
        capture_ = enabled;
        head_ = used_ = partial_ = 0;
        ready_ = false;
    }
    void Fail(Fault f) {
        if (fault_ == Fault::None)
            fault_ = f;
        ready_ = false;
    }
    bool Tx(int64_t first, int64_t stamp, const int16_t* data, size_t n) {
        if (!Validate(tx_, first, stamp, data, n))
            return false;
        if (!reference_.Append(generation_, first, data, n)) {
            Fail(Fault::Sequence);
            return false;
        }
        return true;
    }
    bool Rx(int64_t first, int64_t stamp, const int16_t* data, size_t n) {
        if (!Validate(rx_, first, stamp, data, n))
            return false;
        if (!capture_)
            return true;
        if (used_ == kRxBlocks) {
            Fail(Fault::Overflow);
            return false;
        }
        auto& block = rx_blocks_[(head_ + used_) % kRxBlocks];
        block.first = first;
        block.stamp = stamp;
        for (size_t i = 0; i < n; ++i)
            block.pcm[i] = data[i];
        ++used_;
        return true;
    }
    Result Take(int16_t* mr, size_t n, int64_t now) {
        if (!mr || n == 0 || n > kBlock || n > kBlock - partial_) {
            Fail(Fault::Format);
            return Result::Fault;
        }
        if (fault_ != Fault::None)
            return Result::Fault;
        if (!capture_ || used_ == 0)
            return Result::Wait;
        const auto& block = rx_blocks_[head_];
        if (!tx_.started)
            return WaitOrFault(now, block.stamp);
        const int64_t delta = rx_.origin - tx_.origin;
        if (std::llabs(delta) > kHoldbackUs) {
            Fail(Fault::Cadence);
            return Result::Fault;
        }
        const int64_t phase = delta >= 0 ? (delta * kRate + 500000) / 1000000
                                         : -((-delta * kRate + 500000) / 1000000);
        const int64_t first = block.first + static_cast<int64_t>(partial_) + phase - offset_;
        if (first < 0) {
            Pop();
            return Result::Warmup;
        }
        // Read into a small task/ISR-shared scratch, not a second reference history.
        auto status = reference_.Read(generation_, first, scratch_.data(), n);
        if (status == PlaybackReferenceBuffer::ReadStatus::NotYetAvailable)
            return WaitOrFault(now, block.stamp);
        if (status != PlaybackReferenceBuffer::ReadStatus::Ready) {
            Fail(Fault::Expired);
            return Result::Fault;
        }
        for (size_t i = 0; i < n; ++i) {
            mr[2 * i] = block.pcm[partial_ + i];
            mr[2 * i + 1] = scratch_[i];
        }
        partial_ += n;
        if (partial_ == kBlock)
            Pop();
        ready_ = true;
        pairs_ += n;
        return Result::Ready;
    }
    bool eligible() const { return capture_ && ready_ && fault_ == Fault::None; }
    Fault fault() const { return fault_; }
    uint32_t generation() const { return generation_; }
    int offset() const { return offset_; }
    int64_t tx_samples() const { return tx_.end; }
    int64_t rx_samples() const { return rx_.end; }
    int64_t phase_us() const { return rx_.origin - tx_.origin; }
    int64_t paired_samples() const { return pairs_; }
    size_t pending_blocks() const { return used_; }
    size_t front_remaining() const { return kBlock - partial_; }

private:
    struct Clock {
        int64_t end = 0, origin = 0, last_stamp = 0, rate_stamp = 0, rate_end = 0;
        bool started = false;
    };
    struct Block {
        std::array<int16_t, kBlock> pcm{};
        int64_t first = 0, stamp = 0;
    };
    bool Validate(Clock& clock, int64_t first, int64_t stamp, const int16_t* pcm, size_t n) {
        if (fault_ != Fault::None)
            return false;
        if (!pcm || n != kBlock || stamp < 0 || stamp > INT64_MAX - 1000000 || first < 0 ||
            first > INT64_MAX - static_cast<int64_t>(kBlock)) {
            Fail(Fault::Format);
            return false;
        }
        if (first != clock.end) {
            Fail(Fault::Sequence);
            return false;
        }
        // Avoid overflow even in an impossible multi-million-year epoch.
        if (first > INT64_MAX / 1000000 - static_cast<int64_t>(kBlock)) {
            Fail(Fault::Format);
            return false;
        }
        const int64_t duration = (first + static_cast<int64_t>(n)) * 1000000 / kRate;
        if (!clock.started) {
            clock.origin = stamp - duration;
            clock.started = true;
            clock.rate_stamp = stamp;
            clock.rate_end = first + n;
        } else {
            // Adjacent missing/repeated callbacks are gaps; shared slow clock
            // error must not accumulate against the first nominal timestamp.
            if (std::llabs(stamp - clock.last_stamp - 10000) >
                kCadenceToleranceUs + 10000 * kMaxClockErrorPpm / 1000000) {
                Fail(Fault::Cadence);
                return false;
            }
            const int64_t window = (first + n - clock.rate_end) * 1000000 / kRate;
            if (window >= kRateWindowUs) {
                const auto error = stamp - clock.rate_stamp - window;
                if (std::llabs(error) >
                    window / 1000000 * kMaxClockErrorPpm + kCadenceToleranceUs) {
                    Fail(Fault::Cadence);
                    return false;
                }
                clock.rate_stamp = stamp;
                clock.rate_end = first + n;
            }
        }
        clock.end += n;
        clock.last_stamp = stamp;
        if (tx_.started && rx_.started) {
            // Compare latest observations at equal integer sample position.
            // The initial phase stays fixed; only common clock error cancels.
            const auto samples = rx_.end - tx_.end;
            const auto elapsed = samples * 1000000 / kRate;
            const auto relative =
                (rx_.last_stamp - tx_.last_stamp) - elapsed - (rx_.origin - tx_.origin);
            const auto common_skew = std::llabs(elapsed) / 1000000 * kMaxClockErrorPpm +
                                     (std::llabs(elapsed) % 1000000) * kMaxClockErrorPpm / 1000000;
            if (std::llabs(relative) > kCadenceToleranceUs + common_skew) {
                Fail(Fault::Cadence);
                return false;
            }
        }
        return true;
    }
    Result WaitOrFault(int64_t now, int64_t stamp) {
        if (now < stamp || now - stamp > kHoldbackUs) {
            Fail(Fault::Timeout);
            return Result::Fault;
        }
        return Result::Wait;
    }
    void Pop() {
        head_ = (head_ + 1) % kRxBlocks;
        --used_;
        partial_ = 0;
    }
    // PCM history + reference scratch + TX clock/state metadata stay under32KiB.
    static_assert(sizeof(PlaybackReferenceBuffer) + kBlock * sizeof(int16_t) + 160 <= 32768);
    PlaybackReferenceBuffer reference_;
    std::array<Block, kRxBlocks> rx_blocks_{};
    std::array<int16_t, kBlock> scratch_{};
    Clock tx_, rx_;
    uint32_t generation_ = 0;
    int offset_ = 0;
    Fault fault_ = Fault::None;
    size_t head_ = 0, used_ = 0, partial_ = 0;
    int64_t pairs_ = 0;
    bool capture_ = false, ready_ = false;
};
}  // namespace family_robot

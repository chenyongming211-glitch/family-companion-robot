#pragma once

#include <array>
#include <cstddef>
#include <cstdint>
#include <limits>

namespace family_robot {

// Caller synchronizes every operation with one ISR/task-safe critical section,
// including Reset, Read and diagnostic accessors. No internal locking is provided.
// Indices describe the playback sample timeline, not arrival or network time.
class PlaybackReferenceBuffer {
public:
    static constexpr size_t kCapacity = 16000;
    enum class ReadStatus {
        Ready,
        NotYetAvailable,
        Expired,
        DifferentGeneration,
        InvalidRange,
    };

    PlaybackReferenceBuffer() = default;
    PlaybackReferenceBuffer(const PlaybackReferenceBuffer&) = delete;
    PlaybackReferenceBuffer& operator=(const PlaybackReferenceBuffer&) = delete;

    void Reset(uint32_t generation) {
        generation_ = generation;
        begin_ = 0;
        end_ = 0;
    }

    bool Append(uint32_t generation, int64_t first_sample, const int16_t* pcm,
                size_t count) {
        if (generation != generation_ || first_sample != end_ ||
            !ValidRange(first_sample, count) || (count != 0 && pcm == nullptr)) {
            return false;
        }
        for (size_t i = 0; i < count; ++i) {
            const auto sample = first_sample + static_cast<int64_t>(i);
            samples_[static_cast<size_t>(sample % static_cast<int64_t>(kCapacity))] = pcm[i];
        }
        end_ += static_cast<int64_t>(count);
        if (end_ - begin_ > static_cast<int64_t>(kCapacity)) {
            begin_ = end_ - static_cast<int64_t>(kCapacity);
        }
        return true;
    }

    ReadStatus Read(uint32_t generation, int64_t first_sample, int16_t* output,
                    size_t count) const {
        if (generation != generation_) return ReadStatus::DifferentGeneration;
        if (!ValidRange(first_sample, count) || (count != 0 && output == nullptr)) {
            return ReadStatus::InvalidRange;
        }
        if (first_sample < begin_) return ReadStatus::Expired;
        if (first_sample + static_cast<int64_t>(count) > end_) {
            return ReadStatus::NotYetAvailable;
        }
        for (size_t i = 0; i < count; ++i) {
            const auto sample = first_sample + static_cast<int64_t>(i);
            output[i] = samples_[static_cast<size_t>(sample % static_cast<int64_t>(kCapacity))];
        }
        return ReadStatus::Ready;
    }

    int64_t begin_sample() const { return begin_; }
    int64_t end_sample() const { return end_; }
    uint32_t generation() const { return generation_; }
    size_t size() const { return static_cast<size_t>(end_ - begin_); }

private:
    static bool ValidRange(int64_t first_sample, size_t count) {
        return first_sample >= 0 && count <= kCapacity &&
               static_cast<int64_t>(count) <=
                   std::numeric_limits<int64_t>::max() - first_sample;
    }

    std::array<int16_t, kCapacity> samples_{};
    int64_t begin_ = 0;
    int64_t end_ = 0;
    uint32_t generation_ = 0;
};

static_assert(sizeof(PlaybackReferenceBuffer) <= 32768);

}  // namespace family_robot

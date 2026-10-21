#include "playback_reference_buffer.h"

#include <array>
#include <cstdint>
#include <iostream>
#include <limits>
#include <stdexcept>
#include <vector>

using family_robot::PlaybackReferenceBuffer;
using Status = PlaybackReferenceBuffer::ReadStatus;
static_assert(sizeof(PlaybackReferenceBuffer) <= 32768);
static_assert(PlaybackReferenceBuffer::kCapacity == 16000);

static void Check(bool condition, const char* message) {
    if (!condition) throw std::runtime_error(message);
}

struct Snapshot {
    uint32_t generation;
    int64_t begin;
    int64_t end;
    std::vector<int16_t> samples;
};

static Snapshot Save(const PlaybackReferenceBuffer& buffer) {
    Snapshot saved{buffer.generation(), buffer.begin_sample(), buffer.end_sample(),
                   std::vector<int16_t>(buffer.size())};
    Check(buffer.Read(saved.generation, saved.begin, saved.samples.data(),
                      saved.samples.size()) == Status::Ready, "snapshot unreadable");
    return saved;
}

static void Unchanged(const PlaybackReferenceBuffer& buffer, const Snapshot& before) {
    const auto after = Save(buffer);
    Check(before.generation == after.generation && before.begin == after.begin &&
          before.end == after.end && before.samples == after.samples,
          "rejected operation mutated history");
}

static void ExactAndSplitWindow() {
    PlaybackReferenceBuffer buffer;
    buffer.Reset(7);
    const int16_t a[] = {11, 12, 13};
    const int16_t b[] = {21, 22, 23};
    Check(buffer.Append(7, 0, a, 3), "first block rejected");
    std::array<int16_t, 3> exact{};
    Check(buffer.Read(7, 0, exact.data(), exact.size()) == Status::Ready,
          "exact window unavailable");
    Check((exact == std::array<int16_t, 3>{11, 12, 13}), "exact samples differ");
    Check(buffer.Append(7, 3, b, 3), "second block rejected");
    std::array<int16_t, 4> result{};
    Check(buffer.Read(7, 1, result.data(), result.size()) == Status::Ready,
          "split window unavailable");
    Check((result == std::array<int16_t, 4>{12, 13, 21, 22}), "split sample order");
    Check(buffer.begin_sample() == 0 && buffer.end_sample() == 6 && buffer.size() == 6,
          "wrong timeline bounds");
}

static void WrapAndExpiry() {
    PlaybackReferenceBuffer buffer;
    buffer.Reset(8);
    std::vector<int16_t> ramp(PlaybackReferenceBuffer::kCapacity);
    for (size_t i = 0; i < ramp.size(); ++i) ramp[i] = static_cast<int16_t>(i);
    Check(buffer.Append(8, 0, ramp.data(), ramp.size()), "capacity block rejected");
    const int16_t tail[] = {16000, 16001, 16002};
    Check(buffer.Append(8, 16000, tail, 3), "wrap block rejected");
    Check(buffer.begin_sample() == 3 && buffer.end_sample() == 16003 &&
          buffer.size() == 16000, "wrap bounds wrong");
    std::vector<int16_t> result(16000, -1);
    Check(buffer.Read(8, 3, result.data(), result.size()) == Status::Ready,
          "wrapped full window unavailable");
    for (size_t i = 0; i < result.size(); ++i)
        Check(result[i] == static_cast<int16_t>(i + 3), "wrapped ramp order wrong");
    std::array<int16_t, 4> sentinel{91, 92, 93, 94};
    const auto original = sentinel;
    Check(buffer.Read(8, 2, sentinel.data(), sentinel.size()) == Status::Expired,
          "expired crossing window accepted");
    Check(sentinel == original, "expired read copied partial output");
}

static void FutureAndIncomplete() {
    PlaybackReferenceBuffer buffer;
    buffer.Reset(9);
    const int16_t pcm[] = {31, 32, 33};
    Check(buffer.Append(9, 0, pcm, 3), "future setup rejected");
    std::array<int16_t, 3> output{101, 102, 103};
    const auto sentinel = output;
    Check(buffer.Read(9, 4, output.data(), output.size()) == Status::NotYetAvailable,
          "future read accepted");
    Check(output == sentinel, "future read changed output");
    Check(buffer.Read(9, 1, output.data(), output.size()) == Status::NotYetAvailable,
          "incomplete read accepted");
    Check(output == sentinel, "incomplete read copied available prefix");
}

static void GenerationAndContiguity() {
    PlaybackReferenceBuffer buffer;
    buffer.Reset(10);
    const int16_t pcm[] = {41, 42, 43};
    Check(buffer.Append(10, 0, pcm, 3), "generation setup rejected");
    const auto before = Save(buffer);
    Check(!buffer.Append(11, 3, pcm, 3), "wrong generation append accepted");
    Unchanged(buffer, before);
    std::array<int16_t, 3> output{111, 112, 113};
    const auto sentinel = output;
    Check(buffer.Read(11, 0, output.data(), output.size()) == Status::DifferentGeneration,
          "wrong generation read accepted");
    Check(output == sentinel, "wrong generation changed output");
    Unchanged(buffer, before);
    Check(!buffer.Append(10, 4, pcm, 3), "append gap accepted");
    Unchanged(buffer, before);
    Check(!buffer.Append(10, 2, pcm, 3), "append overlap accepted");
    Unchanged(buffer, before);
    Check(buffer.Append(10, 3, pcm, 3), "contiguous append after rejections failed");
}

static void ExplicitZerosAndSubmittedTail() {
    PlaybackReferenceBuffer buffer;
    buffer.Reset(12);
    const int16_t tail[] = {51, -52, 53};
    const int16_t zeros[] = {0, 0, 0};
    Check(buffer.Append(12, 0, tail, 3), "submitted tail rejected");
    std::array<int16_t, 6> output{121, 122, 123, 124, 125, 126};
    const auto sentinel = output;
    Check(buffer.Read(12, 0, output.data(), output.size()) == Status::NotYetAvailable,
          "missing silence fabricated");
    Check(output == sentinel, "missing silence changed output");
    Check(buffer.Append(12, 3, zeros, 3), "explicit zeros rejected");
    Check(buffer.Read(12, 0, output.data(), output.size()) == Status::Ready,
          "tail followed by zeros lost");
    Check((output == std::array<int16_t, 6>{51, -52, 53, 0, 0, 0}),
          "submitted tail or explicit zeros changed");
    Check(buffer.generation() == 12 && buffer.begin_sample() == 0 &&
          buffer.end_sample() == 6, "tail timeline reset");
}

static void InvalidRangesAndOverflow() {
    PlaybackReferenceBuffer buffer;
    buffer.Reset(13);
    const int16_t pcm[] = {61, 62, 63};
    Check(buffer.Append(13, 0, pcm, 3), "range setup rejected");
    const auto before = Save(buffer);
    const auto max_index = std::numeric_limits<int64_t>::max();
    const auto max_count = std::numeric_limits<size_t>::max();
    Check(!buffer.Append(13, -1, pcm, 1), "negative append accepted");
    Unchanged(buffer, before);
    Check(!buffer.Append(13, 3, nullptr, 1), "null append accepted");
    Unchanged(buffer, before);
    Check(!buffer.Append(13, 3, pcm, 16001), "oversized append accepted");
    Unchanged(buffer, before);
    Check(!buffer.Append(13, 3, pcm, max_count), "SIZE_MAX append accepted");
    Unchanged(buffer, before);
    Check(!buffer.Append(13, max_index - 1, pcm, 3), "overflow append accepted");
    Unchanged(buffer, before);
    std::array<int16_t, 3> output{131, 132, 133};
    const auto sentinel = output;
    Check(buffer.Read(13, -1, output.data(), 3) == Status::InvalidRange,
          "negative read accepted");
    Check(output == sentinel, "negative read changed output");
    Check(buffer.Read(13, 0, nullptr, 1) == Status::InvalidRange, "null read accepted");
    Check(buffer.Read(13, 0, output.data(), 16001) == Status::InvalidRange,
          "oversized read accepted");
    Check(output == sentinel, "oversized read changed output");
    Check(buffer.Read(13, 0, output.data(), max_count) == Status::InvalidRange,
          "SIZE_MAX read accepted");
    Check(output == sentinel, "SIZE_MAX read changed output");
    std::vector<int16_t> full(16000, -2345);
    const auto full_sentinel = full;
    Check(buffer.Read(13, max_index - 1, full.data(), full.size()) == Status::InvalidRange,
          "signed overflow read accepted");
    Check(full == full_sentinel, "overflow read changed output");
    Unchanged(buffer, before);
}

static void ResetAndEmptyRanges() {
    PlaybackReferenceBuffer buffer;
    buffer.Reset(14);
    const int16_t pcm[] = {71, 72};
    Check(buffer.Append(14, 0, pcm, 2), "reset setup rejected");
    buffer.Reset(15);
    Check(buffer.generation() == 15 && buffer.begin_sample() == 0 &&
          buffer.end_sample() == 0 && buffer.size() == 0, "reset bounds wrong");
    std::array<int16_t, 2> output{141, 142};
    const auto sentinel = output;
    Check(buffer.Read(14, 0, output.data(), 2) == Status::DifferentGeneration,
          "old generation survived reset");
    Check(output == sentinel, "old generation after reset changed output");
    Check(buffer.Read(15, 0, output.data(), 2) == Status::NotYetAvailable,
          "old PCM exposed under new generation");
    Check(output == sentinel, "reset fabricated output");
    Check(buffer.Append(15, 0, nullptr, 0), "empty append rejected");
    Check(buffer.Read(15, 0, nullptr, 0) == Status::Ready, "empty read rejected");
    Check(buffer.size() == 0 && buffer.end_sample() == 0, "empty append advanced clock");
    const int16_t fresh[] = {81, 82};
    Check(buffer.Append(15, 0, fresh, 2), "new generation append rejected");
    Check(buffer.Read(15, 0, output.data(), 2) == Status::Ready, "new samples missing");
    Check((output == std::array<int16_t, 2>{81, 82}), "new generation data wrong");
}

int main() {
    try {
        ExactAndSplitWindow();
        WrapAndExpiry();
        FutureAndIncomplete();
        GenerationAndContiguity();
        ExplicitZerosAndSubmittedTail();
        InvalidRangesAndOverflow();
        ResetAndEmptyRanges();
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}

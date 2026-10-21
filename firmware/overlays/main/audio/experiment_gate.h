#pragma once
#include <cstdint>
namespace family_robot {
// Shared by the encode worker, consumer and main-task direct wake batch guard.
inline bool WakeBatchValid(uint32_t batch, uint32_t current_batch, uint32_t generation,
                           uint32_t current_generation) {
    return batch == current_batch && generation == current_generation;
}
// Caller serializes these lifecycle transitions; adapter publishes32-bit snapshots.
class CaptureEpoch {
public:
    void HardwareChange() {
        ++capture_;
        ++wake_;
    }
    void Fault() {
        ++capture_;
        ++wake_;
    }
    void Pause() { ++capture_; }
    void Resume() {
        ++capture_;
        ++wake_;
    }
    uint32_t capture() const { return capture_; }
    uint32_t wake() const { return wake_; }

private:
    uint32_t capture_ = 0, wake_ = 0;
};

// Owned under AudioService::audio_queue_mutex_. Playback reset intentionally does
// not touch this gate: an interrupted TTS must preserve the live mic epoch.
class ExperimentGate {
public:
    bool Set(bool enabled, uint32_t capture_generation) {
        if (enabled_ == enabled && capture_generation_ == capture_generation)
            return false;
        enabled_ = enabled;
        capture_generation_ = capture_generation;
        ++epoch_;
        return true;
    }
    bool Allows(uint32_t epoch) const { return enabled_ && epoch == epoch_; }
    uint32_t epoch() const { return epoch_; }

private:
    bool enabled_ = false;
    uint32_t capture_generation_ = 0, epoch_ = 0;
};
}  // namespace family_robot

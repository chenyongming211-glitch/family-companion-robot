#pragma once
#include <cstdint>
#include <mutex>
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
        ++integrity_;
        ++capture_;
        ++wake_;
    }
    void Fault() {
        ++integrity_;
        ++capture_;
        ++wake_;
    }
    void Pause() { ++capture_; }
    void Boundary() { ++capture_; }  // discard raw backlog, not a new fault/wake invocation
    uint32_t integrity() const { return integrity_; }
    void Resume() {
        ++capture_;
        ++wake_;
    }
    uint32_t capture() const { return capture_; }
    uint32_t wake() const { return wake_; }

private:
    uint32_t capture_ = 0, wake_ = 0, integrity_ = 0;
};

// Guarded with the send gate by AudioService's queue mutex. A new explicit
// auto-listening request authorizes mono for this integrity transition only;
// intentional capture pause/warmup/resume must not revoke that authorization.
class UplinkAuthorization {
public:
    void Request(bool enabled, uint32_t integrity) {
        requested_ = enabled;
        authorized_integrity_ = integrity;
    }
    bool Allows(bool faulted, uint32_t integrity) const {
        return requested_ && (!faulted || integrity == authorized_integrity_);
    }

private:
    bool requested_ = false;
    uint32_t authorized_integrity_ = 0;
};

// Serialize the source-of-truth read AND apply, not just the codec mutation.
// All consumers (input, main, AFE) use this owner; warmup is a persistent hold.
class CaptureActivation {
public:
    template <class Read, class Apply>
    void Refresh(Read read, Apply apply) {
        std::lock_guard<std::mutex> lock(mutex_);
        apply(!warmup_ && read());
    }
    template <class Read, class Apply>
    void Warmup(bool hold, Read read, Apply apply) {
        std::lock_guard<std::mutex> lock(mutex_);
        warmup_ = hold;
        apply(!warmup_ && read());
    }

private:
    std::mutex mutex_;
    bool warmup_ = false;
};

// Observer-auto control quarantine, not a physical/acoustic drain measurement.
// Starts once writes/queues are idle; repeated idle polling cannot extend it.
class ObserverTail {
public:
    static constexpr int64_t kGuardUs = 120000;
    void Begin(bool quarantine) {
        pending_ = quarantine;
        idle_started_ = false;
    }
    bool pending() const { return pending_; }
    bool Advance(int64_t now, bool software_idle, uint32_t output_activity = 0) {
        if (!pending_)
            return false;
        if (activity_ != output_activity) {
            idle_started_ = false;
            activity_ = output_activity;
        }
        if (!software_idle) {
            idle_started_ = false;
            return false;
        }
        if (!idle_started_) {
            idle_started_ = true;
            idle_since_ = now;
        }
        if (now < idle_since_ || now - idle_since_ < kGuardUs)
            return false;
        pending_ = false;
        return true;  // caller must flush raw backlog and invalidate generation
    }

private:
    bool pending_ = false, idle_started_ = false;
    int64_t idle_since_ = 0;
    uint32_t activity_ = 0;
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

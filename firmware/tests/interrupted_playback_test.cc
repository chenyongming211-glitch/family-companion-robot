#include <cassert>
#include "experiment_gate.h"
int main() {
    family_robot::ExperimentGate g;
    assert(!g.Allows(g.epoch()));
    g.Set(true, 1);
    const auto old = g.epoch();
    assert(g.Allows(old));
    g.Set(false, 1);
    assert(!g.Allows(old));  // observer speaking: drop queued/in-flight encode
    g.Set(true, 1);
    assert(!g.Allows(old));
    assert(g.Allows(g.epoch()));
    const auto restart = g.epoch();
    g.Set(true, 2);
    assert(!g.Allows(restart));
    // An interrupted playback reset does not call Set: live microphone epoch survives.
    const auto mic = g.epoch();
    assert(g.Allows(mic));
    g.Set(false, 2);
    assert(!g.Allows(mic));  // fault closes immediately
    family_robot::CaptureEpoch capture;
    capture.HardwareChange();
    capture.Resume();
    const auto live = capture.capture(), wake = capture.wake();
    capture.Pause();
    assert(capture.capture() != live && capture.wake() == wake);
    assert(family_robot::WakeBatchValid(
        1, 1, wake, capture.wake()));  // normal standby snapshot survives suspend
    capture.Fault();
    assert(!family_robot::WakeBatchValid(1, 1, wake, capture.wake()));
    const auto fallback = capture.wake();
    assert(
        family_robot::WakeBatchValid(2, 2, fallback, capture.wake()));  // valid mono fallback wake
    assert(
        !family_robot::WakeBatchValid(1, 2, fallback, capture.wake()));  // previous worker rejected
    capture.Resume();
    assert(!family_robot::WakeBatchValid(2, 2, fallback, capture.wake()));
    const auto before_restart = capture.wake();
    capture.HardwareChange();
    assert(!family_robot::WakeBatchValid(3, 3, before_restart, capture.wake()));
}

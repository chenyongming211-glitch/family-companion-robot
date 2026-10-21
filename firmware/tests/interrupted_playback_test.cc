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

    // Full failed-reference auto lifecycle: authorization belongs to this fault,
    // not the capture generation sampled before the start/warmup handshake.
    family_robot::CaptureEpoch lifecycle;
    family_robot::UplinkAuthorization authorization;
    family_robot::ExperimentGate send;
    lifecycle.HardwareChange();
    lifecycle.Resume();
    authorization.Request(true, lifecycle.integrity());
    send.Set(authorization.Allows(false, lifecycle.integrity()), lifecycle.capture());
    const auto old_aec = send.epoch();
    lifecycle.Fault();
    send.Set(authorization.Allows(true, lifecycle.integrity()), lifecycle.capture());
    assert(!send.Allows(old_aec));
    authorization.Request(false, lifecycle.integrity());  // idle
    lifecycle.Pause();
    lifecycle.Resume();  // mono fallback wake detection
    const auto snapshot = lifecycle.wake();
    lifecycle.Pause();  // frozen wake snapshot
    assert(family_robot::WakeBatchValid(1, 1, snapshot, lifecycle.wake()));
    authorization.Request(true, lifecycle.integrity());  // StartListeningAudio before warmup
    const auto fault_identity = lifecycle.integrity();
    lifecycle.Pause();
    lifecycle.Resume();  // warmup completes, first fresh mono frame
    assert(lifecycle.integrity() == fault_identity);
    send.Set(authorization.Allows(true, lifecycle.integrity()), lifecycle.capture());
    assert(send.Allows(send.epoch()) && !send.Allows(old_aec));
    const auto mono = send.epoch();
    lifecycle.Fault();  // a new fault cannot inherit the old explicit grant
    send.Set(authorization.Allows(true, lifecycle.integrity()), lifecycle.capture());
    assert(!send.Allows(mono) && !send.Allows(send.epoch()));
    authorization.Request(true, lifecycle.integrity());
    lifecycle.HardwareChange();
    assert(!authorization.Allows(true, lifecycle.integrity()));

    family_robot::ObserverTail tail;
    family_robot::ExperimentGate observer;
    lifecycle.Resume();
    const auto raw_before_tail = lifecycle.capture();
    tail.Begin(true);
    assert(!tail.Advance(0, false));      // output still in flight: timer cannot start
    assert(!tail.Advance(500000, true));  // write complete, DMA may still be sounding
    // Even an immediate AFE reset cannot permit new cloud output during the tail.
    observer.Set(!tail.pending(), lifecycle.capture());
    const auto early_afe = observer.epoch();
    assert(!observer.Allows(early_afe));
    assert(!tail.Advance(550000, true));
    assert(!tail.Advance(619999, true));  // repeated idle polls do not postpone deadline
    assert(tail.Advance(620000, true));
    const auto wake_before_boundary = lifecycle.wake();
    lifecycle.Boundary();  // atomic RX flush/capture invalidation, preserving wake snapshot
    assert(lifecycle.wake() == wake_before_boundary);
    observer.Set(!tail.pending(), lifecycle.capture());
    assert(raw_before_tail != lifecycle.capture());
    assert(!observer.Allows(early_afe) && observer.Allows(observer.epoch()));
    assert(!tail.Advance(800000, true));  // exactly one boundary
    tail.Begin(true);
    assert(!tail.Advance(900000, true));
    assert(!tail.Advance(1000000, false));  // new output invalidates previous idle deadline
    assert(!tail.Advance(1100000, true));
    assert(!tail.Advance(1219999, true));
    assert(tail.Advance(1220000, true));
    tail.Begin(true);
    assert(!tail.Advance(2000000, true, 10));
    // A short write can begin/end between polls: activity token restarts guard.
    assert(!tail.Advance(2110000, true, 11));
    assert(!tail.Advance(2120000, true, 11));
    assert(tail.Advance(2230000, true, 11));
    tail.Begin(false);  // valid realtime path has no observer quarantine
    assert(!tail.pending() && !tail.Advance(1300000, true));
}

#include <atomic>
#include <cassert>
#include <future>
#include <thread>
#include "duplex_reference.h"
#include "experiment_gate.h"
int main() {
    family_robot::CaptureActivation policy;
    family_robot::DuplexReference pair;
    pair.Begin(1, 0);
    std::atomic<bool> active{true};
    bool armed = false;
    auto apply = [&](bool value) {
        armed = value;
        pair.Capture(value);
    };
    auto read = [&] { return active.load(); };
    std::promise<void> read_old, release, disable_started, disable_done;
    auto done = disable_done.get_future();
    auto release_future = release.get_future();
    std::thread old([&] {
        policy.Refresh(
            [&] {
                const bool snapshot = active.load();
                read_old.set_value();
                release_future.wait();
                return snapshot;
            },
            apply);
    });
    read_old.get_future().wait();
    active = false;
    std::thread disable([&] {
        disable_started.set_value();
        policy.Refresh(read, apply);
        disable_done.set_value();
    });
    disable_started.get_future().wait();
    // Once the old read owns the policy, disable cannot complete ahead of its apply.
    assert(done.wait_for(std::chrono::milliseconds(100)) == std::future_status::timeout);
    release.set_value();
    old.join();
    disable.join();
    assert(!armed);               // old activation cannot apply AFTER disabling Refresh completes
    policy.Refresh(read, apply);  // delayed caller reads current bits, not cached bits
    assert(!armed);
    active = true;
    policy.Warmup(true, read, apply);
    policy.Refresh(read, apply);
    assert(!armed);  // another lifecycle caller cannot undo warmup's hold
    active = false;
    policy.Warmup(false, read, apply);
    assert(!armed);
    int16_t pcm[240]{};
    for (int b = 0; b < 40; ++b) {
        assert(pair.Tx(b * 240, (b + 1) * 10000, pcm, 240));
        assert(pair.Rx(b * 240, (b + 1) * 10000, pcm, 240));
    }
    assert(pair.pending_blocks() == 0 &&
           pair.fault() == family_robot::DuplexReference::Fault::None);
}

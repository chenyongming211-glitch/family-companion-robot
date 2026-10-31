#include "button_uplink.h"
#include <cassert>
#include <chrono>
#include <condition_variable>
#include <future>
#include <mutex>
#include <thread>

int main() {
    family_robot::ButtonUplink gate;
    assert(gate.Capture() == 0 && !gate.Allows(0));
    int flushed = 0, sent = 0;
    assert(gate.Open(gate.Revision(), [&] { assert(gate.Capture() == 0); ++flushed; }));
    const auto first = gate.Capture();
    assert(first && gate.Allows(first) && flushed == 1);
    assert(gate.Send(first, [&] { ++sent; return true; }));
    assert(!gate.Send(first, [] { return false; }));
    gate.Close([&] { assert(!gate.Allows(first)); ++flushed; });
    assert(gate.Capture() == 0);
    assert(gate.Send(first, [&] { ++sent; return false; }));
    const auto revoked = gate.Revision();
    gate.Close([] {});
    assert(!gate.Open(revoked, [] { assert(false); }));
    assert(gate.Open(gate.Revision(), [&] { assert(gate.Capture() == 0); ++flushed; }));
    const auto next = gate.Capture();
    assert(next != first && !gate.Allows(first) && gate.Allows(next));
    assert(gate.Send(first, [&] { ++sent; return false; }) && sent == 1);
    assert(!gate.Open(gate.Revision(), [&] { assert(false); }));
    assert(gate.Capture() == next && flushed == 3);

    // Closing cannot overtake an already authorized transport send. Once it
    // completes, no packet from that epoch may reach the transport again.
    std::mutex mutex;
    std::condition_variable cv;
    bool entered = false, release = false;
    auto sending = std::async(std::launch::async, [&] {
        return gate.Send(next, [&] {
            std::unique_lock<std::mutex> lock(mutex);
            entered = true; cv.notify_all();
            cv.wait(lock, [&] { return release; });
            ++sent; return true;
        });
    });
    {
        std::unique_lock<std::mutex> lock(mutex);
        cv.wait(lock, [&] { return entered; });
    }
    std::promise<void> close_started;
    auto closing = std::async(std::launch::async, [&] {
        close_started.set_value();
        gate.Close([&] { ++flushed; });
    });
    close_started.get_future().wait();
    assert(closing.wait_for(std::chrono::milliseconds(30)) == std::future_status::timeout);
    { std::lock_guard<std::mutex> lock(mutex); release = true; }
    cv.notify_all();
    assert(sending.get()); closing.get();
    assert(gate.Capture() == 0 && sent == 2 && flushed == 4);
    assert(gate.Send(next, [&] { ++sent; return false; }) && sent == 2);
    assert(gate.Open(gate.Revision(), [] {}));
    assert(!gate.Allows(next) && gate.Allows(gate.Capture()));
}

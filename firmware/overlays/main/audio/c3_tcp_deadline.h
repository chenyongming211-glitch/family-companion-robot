#pragma once
#include <cerrno>
#include <cstddef>
#include <cstdint>
#include <limits>

namespace family::c3::network {
inline constexpr int64_t kSendBudgetUs = 250000;
inline constexpr int64_t kConnectBudgetUs = 5000000;
struct WriteResult { std::ptrdiff_t bytes; int error; };
struct WaitResult { int ready; int error; }; // ready: positive, timeout: 0, error: -1

// Now returns monotonic microseconds. Write never blocks; Wait accepts the
// remaining total budget. No partial write or EINTR restarts the deadline.
template<class Now, class Write, class Wait>
bool SendAll(const char* data, size_t size, int64_t budget_us, Now now,
             Write write, Wait wait, int& error) {
    error = 0;
    if (budget_us <= 0 || size > static_cast<size_t>(std::numeric_limits<int>::max())) {
        error = EINVAL; return false;
    }
    const int64_t start = now();
    if (start < 0) { error = EIO; return false; }
    if (start > std::numeric_limits<int64_t>::max() - budget_us) {
        error = EINVAL; return false;
    }
    const int64_t deadline = start + budget_us;
    size_t sent = 0;
    while (sent < size) {
        const int64_t current = now();
        if (current < 0) { error = EIO; return false; }
        if (current >= deadline) { error = ETIMEDOUT; return false; }
        const WriteResult result = write(data + sent, size - sent);
        if (result.bytes > 0) {
            if (static_cast<size_t>(result.bytes) > size - sent) { error = EIO; return false; }
            sent += static_cast<size_t>(result.bytes);
            // A write completing at/after the deadline cannot claim success.
            const int64_t completed = now();
            if (completed < 0) { error = EIO; return false; }
            if (completed >= deadline) { error = ETIMEDOUT; return false; }
            continue;
        }
        if (result.bytes == 0) { error = EPIPE; return false; }
        if (result.error == EINTR) continue;
        if (result.error != EAGAIN && result.error != EWOULDBLOCK) {
            error = result.error ? result.error : EIO; return false;
        }
        const int64_t waiting = now();
        if (waiting < 0) { error = EIO; return false; }
        const int64_t remaining = deadline - waiting;
        if (remaining <= 0) { error = ETIMEDOUT; return false; }
        const WaitResult ready = wait(remaining);
        if (ready.ready == 0) { error = ETIMEDOUT; return false; }
        if (ready.ready < 0 && ready.error != EINTR) { error = ready.error; return false; }
    }
    return true;
}
} // namespace family::c3::network

#pragma once
#include <atomic>
#include <cstdint>
#include <mutex>

namespace family_robot {
// A capture token survives encoding, but never a close/reopen. The dispatch
// lock linearizes the final transport write with closure; queue operations do
// not take this lock, so workers may check their tokens without lock inversion.
class ButtonUplink {
public:
    uint32_t Revision() const { return epoch_.load(); }
    uint32_t Capture() const {
        const auto value = epoch_.load();
        return (value & 1u) && value > revoked_.load() ? value : 0;
    }
    bool Allows(uint32_t token) const {
        return token != 0 && token == Capture();
    }
    // High-priority capture faults cannot wait behind synchronous transport.
    // Monotonic max means a delayed old fault cannot un-revoke a newer epoch.
    void Revoke(uint32_t token) {
        if (!(token & 1u)) return;
        auto previous = revoked_.load();
        while (previous < token && !revoked_.compare_exchange_weak(previous, token)) {}
    }
    template <class Flush>
    void Close(Flush flush) {
        std::lock_guard<std::mutex> lock(dispatch_);
        const auto previous = epoch_.load();
        if (previous >= UINT32_MAX - 1u) return;
        // Even an already-closed receive event revokes a pending reopen ticket.
        epoch_.store(previous + ((previous & 1u) ? 1u : 2u));
        flush();
    }
    template <class Flush>
    bool CloseRevoked(uint32_t token, Flush flush) {
        std::lock_guard<std::mutex> lock(dispatch_);
        if (epoch_.load() != token || token >= UINT32_MAX-1u ||
            ((token & 1u) && revoked_.load() < token)) return false;
        epoch_.store(token + ((token & 1u) ? 1u : 2u)); // Open never issues UINT32_MAX.
        flush();
        return true;
    }
    template <class Flush>
    bool Open(uint32_t expected, Flush flush) {
        std::lock_guard<std::mutex> lock(dispatch_);
        if ((expected & 1u) || expected >= UINT32_MAX - 1u || epoch_.load() != expected)
            return false;
        flush();
        epoch_.store(expected + 1u);
        return true;
    }
    template <class Transport>
    bool Send(uint32_t token, Transport transport) {
        std::lock_guard<std::mutex> lock(dispatch_);
        // Policy drops are successful consumption, not network failures.
        return Allows(token) ? transport() : true;
    }
private:
    std::atomic<uint32_t> revoked_{0};
    std::atomic<uint32_t> epoch_{0}; // odd=open, even=closed; never wraps
    std::mutex dispatch_;
};
} // namespace family_robot

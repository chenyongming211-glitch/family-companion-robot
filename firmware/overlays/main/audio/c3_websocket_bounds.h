#pragma once
// C3-only receive policy. Borrowed frame bytes are valid only during callbacks.
#include <cstddef>
#include <cstdint>
#include <string>
#include <vector>

namespace family::c3::network {
inline constexpr size_t kHandshakeBytes = 2048;
inline constexpr size_t kTextBytes = 4096;
inline constexpr size_t kBinaryBytes = 2880;
inline constexpr size_t kReceiveBytes = 6144;
inline constexpr size_t kTcpChunkBytes = 1500;

// Storage belongs to the unchanged SDK object. Constructor reserves the raw
// string; the first fragmented message reserves the fragment vector once.
// These capacities, terminator, allocator and
// object metadata remain part of the device heap budget (not the PCM pool).
template<class Handshake, class Data, class Control, class Failure>
bool Receive(std::string& raw, bool& handshaked, bool& fragmented,
             bool& binary, std::vector<char>& fragments,
             const std::string& chunk, Handshake handshake, Data deliver,
             Control control, Failure failure) {
    auto fail = [&]() { failure(); return false; };
    if (chunk.size() > kTcpChunkBytes || raw.size() > kReceiveBytes ||
        chunk.size() > kReceiveBytes - raw.size()) return fail();
    raw.append(chunk);
    if (!handshaked) {
        const size_t end = raw.find("\r\n\r\n");
        if (end == std::string::npos) {
            if (raw.size() > kHandshakeBytes) return fail();
            return true;
        }
        if (end + 4 > kHandshakeBytes || raw.compare(0, 13, "HTTP/1.1 101 ") != 0)
            return fail();
        raw.erase(0, end + 4);
        handshaked = true;
        handshake();
    }
    size_t offset = 0;
    while (raw.size() - offset >= 2) {
        const auto* p = reinterpret_cast<const uint8_t*>(raw.data() + offset);
        const bool fin = (p[0] & 0x80) != 0;
        const uint8_t op = p[0] & 0x0f;
        const bool is_control = op >= 8;
        if ((p[0] & 0x70) || (p[1] & 0x80) ||
            !(op == 0 || op == 1 || op == 2 || op == 8 || op == 9 || op == 10) ||
            (is_control && !fin) || (op == 0 && !fragmented) ||
            ((op == 1 || op == 2) && fragmented)) return fail();
        uint64_t length = p[1] & 0x7f;
        size_t header = 2;
        if (is_control && length > 125) return fail();
        if (length == 126) {
            if (raw.size() - offset < 4) break;
            length = (uint64_t(p[2]) << 8) | p[3];
            header = 4;
            if (length < 126) return fail();
        } else if (length == 127) {
            if (raw.size() - offset < 10) break;
            length = 0;
            for (size_t i = 0; i < 8; ++i) length = (length << 8) | p[2+i];
            header = 10;
            if ((p[2] & 0x80) || length < 65536) return fail();
        }
        const bool message_binary = op == 0 ? binary : op == 2;
        const size_t limit = message_binary ? kBinaryBytes : kTextBytes;
        const size_t previous = op == 0 ? fragments.size() : 0;
        if (is_control) {
            if (length > 125 || (op == 8 && length == 1)) return fail();
        } else {
            if (previous > limit || length > limit - previous ||
                (fin && message_binary && previous + length != kBinaryBytes)) return fail();
        }
        // Compare by subtraction: hostile 64-bit lengths cannot overflow size_t.
        if (length > raw.size() - offset - header) break;
        const char* payload = raw.data() + offset + header;
        const size_t bytes = static_cast<size_t>(length);
        if (is_control) {
            if (!control(op, payload, bytes)) return false;
        } else if (op != 0 && fin) {
            deliver(payload, bytes, message_binary); // no payload/current_message copy
        } else {
            if (op != 0) {
                fragments.clear();
                if (fragments.capacity() < kTextBytes) fragments.reserve(kTextBytes);
                binary = message_binary; fragmented = true;
            }
            fragments.insert(fragments.end(), payload, payload + bytes);
            if (fin) {
                deliver(fragments.data(), fragments.size(), binary);
                fragments.clear(); fragmented = false;
            }
        }
        offset += header + bytes;
    }
    if (offset) raw.erase(0, offset); // preserve reserved capacity
    return true;
}
} // namespace family::c3::network

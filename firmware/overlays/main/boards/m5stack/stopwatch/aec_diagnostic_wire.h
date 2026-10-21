#pragma once
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <initializer_list>
namespace family_robot {
struct DiagnosticRequest {
    uint8_t address[4]{};
    uint16_t port = 0;
    unsigned seconds = 0;
};
inline bool ParseDiagnosticRequest(const char* line, DiagnosticRequest& r) {
    if (!line || std::strlen(line) > 95)
        return false;
    constexpr char prefix[] = "aec_diag_start ";
    if (std::strncmp(line, prefix, sizeof(prefix) - 1) != 0)
        return false;
    const char* cursor = line + sizeof(prefix) - 1;
    unsigned values[6]{};
    for (unsigned i = 0; i < 6; ++i) {
        if (*cursor < '0' || *cursor > '9')
            return false;
        while (*cursor >= '0' && *cursor <= '9') {
            const unsigned digit = unsigned(*cursor++ - '0');
            if (values[i] > (65535 - digit) / 10)
                return false;
            values[i] = values[i] * 10 + digit;
        }
        const char separator = i < 3 ? '.' : (i < 5 ? ' ' : '\0');
        if (*cursor != separator)
            return false;
        if (separator)
            ++cursor;
    }
    const auto a = values[0], b = values[1], c = values[2], d = values[3], p = values[4],
               s = values[5];
    if (a > 255 || b > 255 || c > 255 || d > 255 || p < 1 || p > 65535 || s < 1 || s > 120)
        return false;
    if (!(a == 10 || (a == 172 && b >= 16 && b <= 31) || (a == 192 && b == 168)))
        return false;
    r.address[0] = a;
    r.address[1] = b;
    r.address[2] = c;
    r.address[3] = d;
    r.port = p;
    r.seconds = s;
    return true;
}
inline void PutLittle(uint8_t* p, uint64_t v, unsigned bytes) {
    for (unsigned i = 0; i < bytes; ++i) {
        p[i] = uint8_t(v);
        v >>= 8;
    }
}
// Fixed endian wire, never compiler struct layout. Timestamp is producer observation
// time, NOT acoustic time; raw and AFE streams have independent sample origins.
inline void WriteDiagnosticHeader(uint8_t* p, uint8_t stream, uint8_t channels, uint8_t flags,
                                  uint32_t seq, uint32_t generation, uint64_t position,
                                  uint64_t timestamp, uint16_t frames, uint16_t payload,
                                  uint32_t drops, uint32_t window) {
    std::memcpy(p, "SWAC", 4);
    p[4] = 1;
    p[5] = stream;
    p[6] = channels;
    p[7] = flags;
    PutLittle(p + 8, seq, 4);
    PutLittle(p + 12, generation, 4);
    PutLittle(p + 16, position, 8);
    PutLittle(p + 24, timestamp, 8);
    PutLittle(p + 32, 16000, 4);
    PutLittle(p + 36, frames, 2);
    PutLittle(p + 38, payload, 2);
    PutLittle(p + 40, drops, 4);
    PutLittle(p + 44, window, 4);
}
}  // namespace family_robot

#include "aec_diagnostic_wire.h"
#include <cassert>
using namespace family_robot;
int main() {
    DiagnosticRequest r{};
    assert(ParseDiagnosticRequest("aec_diag_start 192.168.7.2 9000 120", r));
    assert(r.port == 9000 && r.seconds == 120 && r.address[3] == 2);
    assert(ParseDiagnosticRequest("aec_diag_start 10.1.2.3 1 1", r));
    assert(ParseDiagnosticRequest("aec_diag_start 172.31.1.2 65535 5", r));
    for (auto s : {"aec_diag_start 10.1.2.3 4294967297 1", "aec_diag_start 10.1.2.3 +1 1",
                   "aec_diag_start 172.32.1.2 4 5", "aec_diag_start 8.8.8.8 4 5",
                   "aec_diag_start 192.168.1.2 0 5", "aec_diag_start 192.168.1.2 9000 121",
                   "aec_diag_start 10.1.2.3 1 1 junk", "aec_diag_start 10.1.2.256 1 1",
                   "aec_diag_start 127.0.0.1 1 1", "aec_diag_start 10.0.0.1 65536 1"})
        assert(!ParseDiagnosticRequest(s, r));
    uint8_t header[48]{};
    WriteDiagnosticHeader(header, 1, 2, 1, 7, 8, 0x102030405060708ULL, 123456, 240, 960, 9, 10);
    assert(header[0] == 'S' && header[3] == 'C' && header[4] == 1 && header[5] == 1 &&
           header[6] == 2 && header[7] == 1);
    assert(header[8] == 7 && header[12] == 8 && header[16] == 8 && header[23] == 1);
    assert(header[32] == 0x80 && header[33] == 0x3e && header[36] == 240 && header[38] == 0xc0 &&
           header[39] == 3);
    assert(header[40] == 9 && header[44] == 10);
}

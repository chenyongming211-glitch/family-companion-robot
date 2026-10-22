"""Run the real console reader against the pinned SDK's polling VFS contract."""
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / 'firmware/overlays/main/boards/m5stack/stopwatch/aec_diagnostics.cc'


def console_definition():
    source = SOURCE.read_text()
    start = source.index('void AecDiagnostics::Console()')
    opening = source.index('{', start)
    depth = 1
    end = opening + 1
    while depth:
        depth += (source[end] == '{') - (source[end] == '}')
        end += 1
    return source[start:end]


class UsbConsoleTest(unittest.TestCase):
    def test_stats_command_reaches_parser_without_usb_interrupt_driver(self):
        # IDF fff9895, usb_serial_jtag_vfs.c: usb_serial_jtag_read() prefetches
        # directly from the RX FIFO only when O_NONBLOCK is clear. Its available
        # count comes from usb_serial_jtag_get_read_bytes_available(), which is
        # zero without the optional interrupt driver. The polling RX function
        # itself returns immediately when no FIFO byte exists. Simulate this
        # hardware/VFS boundary, while compiling the actual production loop.
        harness = r'''
#include <cassert>
#include <cerrno>
#include <cstdio>
#include <fcntl.h>
#include <string>
#include <unistd.h>
#include <vector>
class AecDiagnostics {
public:
    bool running_ = true;
    std::vector<std::string> commands;
    void Console();
    void Command(const char* line) { commands.emplace_back(line); running_ = false; }
};
static AecDiagnostics* active;
static bool nonblocking = false;
static std::string rx_fifo = "aec_stats\r\n";
static size_t cursor = 0;
static int idle_polls = 0;
static int PollingFcntl(int fd, int command, int flags) {
    assert(fd == STDIN_FILENO && command == F_SETFL);
    nonblocking = (flags & O_NONBLOCK) != 0;
    return 0;
}
static ssize_t PollingRead(int fd, void* output, size_t count) {
    assert(fd == STDIN_FILENO && count == 1);
    if (nonblocking || cursor == rx_fifo.size()) { errno = EWOULDBLOCK; return -1; }
    *static_cast<char*>(output) = rx_fifo[cursor++];
    return 1;
}
static int pdMS_TO_TICKS(int milliseconds) { return milliseconds; }
static void vTaskDelay(int ticks) {
    assert(ticks == 20);
    if (++idle_polls > 64) active->running_ = false;
}
static void vTaskDelete(void*) {}
#define fcntl PollingFcntl
#define read PollingRead
'''
        harness += console_definition()
        harness += r'''
#undef fcntl
#undef read
int main() {
    AecDiagnostics diagnostics;
    active = &diagnostics;
    diagnostics.Console();
    if (diagnostics.commands != std::vector<std::string>{"aec_stats"}) {
        std::fprintf(stderr, "Expected aec_stats via polling USB VFS; received %zu commands\n",
                     diagnostics.commands.size());
        return 1;
    }
}
'''
        with tempfile.TemporaryDirectory() as directory:
            cpp = Path(directory) / 'console-test.cc'
            binary = Path(directory) / 'console-test'
            cpp.write_text(harness)
            compiled = subprocess.run([
                'c++', '-std=c++17', '-Wall', '-Wextra', '-Werror', '-pedantic',
                str(cpp), '-o', str(binary),
            ], capture_output=True, text=True)
            self.assertEqual(compiled.returncode, 0, compiled.stderr)
            result = subprocess.run([str(binary)], capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr)

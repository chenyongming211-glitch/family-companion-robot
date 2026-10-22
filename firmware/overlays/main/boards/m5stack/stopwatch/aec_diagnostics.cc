#include "aec_diagnostics.h"
#if CONFIG_STOPWATCH_SOFTWARE_AEC
#include <esp_heap_caps.h>
#include <esp_netif.h>
#include <esp_timer.h>
#include <fcntl.h>
#include <lwip/sockets.h>
#include <unistd.h>
#include <algorithm>
#include <cstdio>
#include <cstring>
#include "application.h"
#include "software_aec_codec.h"

bool AecDiagnostics::Start() {
    queue_storage_ = static_cast<uint8_t*>(
        heap_caps_malloc(12 * sizeof(Packet), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT));
    if (!queue_storage_)
        return false;
    queue_ = xQueueCreateStatic(12, sizeof(Packet), queue_storage_, &queue_control_);
    if (!queue_)
        return false;
    running_ = true;
    if (xTaskCreate([](void* p) { static_cast<AecDiagnostics*>(p)->Worker(); }, "aec_udp", 4096,
                    this, 1, &worker_) != pdPASS) {
        running_ = false;
        return false;
    }
    if (xTaskCreate([](void* p) { static_cast<AecDiagnostics*>(p)->Console(); }, "aec_usb", 4096,
                    this, 1, &console_) != pdPASS) {
        running_ = false;
        return false;
    }
    return true;
}
void AecDiagnostics::ResetStream(uint8_t stream) {
    if (stream < 1 || stream > 2)
        return;
    portENTER_CRITICAL(&mux_);
    positions_[stream - 1] = 0;
    discontinuity_[stream - 1] = true;
    portEXIT_CRITICAL(&mux_);
}
void AecDiagnostics::Record(uint8_t stream, const int16_t* pcm, size_t frames, uint8_t channels,
                            uint32_t generation, bool valid) {
    if (!running_ || !pcm || stream < 1 || stream > 2 || channels != (stream == 1 ? 2 : 1))
        return;
    const unsigned index = stream - 1;
    size_t offset = 0;
    while (offset < frames) {
        Packet packet{};
        const size_t n = std::min(frames - offset, size_t(1152 / (2 * channels)));
        portENTER_CRITICAL(&mux_);
        const int64_t now = esp_timer_get_time();
        if (!enabled_ || now >= until_) {
            portEXIT_CRITICAL(&mux_);
            return;
        }
        if (generations_[index] != generation) {
            generations_[index] = generation;
            positions_[index] = 0;
            discontinuity_[index] = true;
        }
        packet.window = window_;
        packet.length = 48 + n * channels * 2;
        family_robot::WriteDiagnosticHeader(packet.bytes, stream, channels,
                                            (valid ? 1 : 0) | (discontinuity_[index] ? 2 : 0),
                                            sequences_[index]++, generation, positions_[index], now,
                                            n, n * channels * 2, drops_.load(), window_);
        positions_[index] += n;
        discontinuity_[index] = false;
        portEXIT_CRITICAL(&mux_);
        for (size_t i = 0; i < n * channels; ++i)
            family_robot::PutLittle(packet.bytes + 48 + i * 2, uint16_t(pcm[offset * channels + i]),
                                    2);
        if (xQueueSend(queue_, &packet, 0) != pdTRUE)
            ++drops_;
        offset += n;
    }
}
void AecDiagnostics::StopCapture() {
    std::lock_guard<std::mutex> destination_lock(destination_mutex_);
    portENTER_CRITICAL(&mux_);
    enabled_ = false;
    ++window_;
    portEXIT_CRITICAL(&mux_);
    if (queue_)
        xQueueReset(queue_);
}
void AecDiagnostics::Worker() {
    int sock = -1;  // No socket/network operation until an explicit capture window.

    while (running_) {
        codec_.PollFault();
        Packet packet{};
        if (xQueueReceive(queue_, &packet, pdMS_TO_TICKS(20)) != pdTRUE)
            continue;
        // Only the low priority worker ever writes to the network. Stop/start is
        // serialized with this nonblocking send, so stop never releases a stale packet.
        std::lock_guard<std::mutex> destination_lock(destination_mutex_);
        portENTER_CRITICAL(&mux_);
        const bool current = enabled_ && packet.window == window_ && esp_timer_get_time() < until_;
        const auto dest = destination_;
        portEXIT_CRITICAL(&mux_);
        if (!current)
            continue;
        if (sock < 0) {
            auto* netif = esp_netif_get_default_netif();
            if (netif && esp_netif_is_netif_up(netif)) {
                sock = socket(AF_INET, SOCK_DGRAM, IPPROTO_UDP);
                if (sock >= 0)
                    fcntl(sock, F_SETFL, O_NONBLOCK);
            }
        }
        sockaddr_in addr{};
        addr.sin_family = AF_INET;
        addr.sin_port = htons(dest.port);
        const uint32_t ip = (uint32_t(dest.address[0]) << 24) | (uint32_t(dest.address[1]) << 16) |
                            (uint32_t(dest.address[2]) << 8) | dest.address[3];
        addr.sin_addr.s_addr = htonl(ip);
        if (sock < 0 || sendto(sock, packet.bytes, packet.length, MSG_DONTWAIT,
                               reinterpret_cast<sockaddr*>(&addr), sizeof(addr)) != packet.length)
            ++drops_;
    }
    if (sock >= 0)
        close(sock);
    vTaskDelete(nullptr);
}
void AecDiagnostics::Command(const char* line) {
    if (std::strcmp(line, "aec_stats") == 0) {
        codec_.PrintStats();
        portENTER_CRITICAL(&mux_);
        const auto window = window_;
        const bool active = enabled_ && esp_timer_get_time() < until_;
        portEXIT_CRITICAL(&mux_);
        printf("aec_diag window=%lu active=%d drops=%lu queue=%u\n", (unsigned long)window, active,
               (unsigned long)drops_.load(), unsigned(uxQueueMessagesWaiting(queue_)));
        return;
    }
    if (std::strcmp(line, "aec_diag_stop") == 0) {
        StopCapture();
        puts("aec_diag stopped");
        return;
    }
    if (std::strcmp(line, "aec_realtime off") == 0) {
        if (realtime_command_pending_.exchange(true)) {
            puts("aec_realtime rejected: command pending");
            return;
        }
        Application::GetInstance().Schedule([this] {
            Application::GetInstance().SetAecMode(kAecOff);
            realtime_command_pending_ = false;
        });
        puts("aec_realtime off queued");
        return;
    }
    if (std::strcmp(line, "aec_realtime on") == 0) {
        if (!codec_.RealtimeEligible()) {
            puts("aec_realtime rejected: not ready or faulted");
            return;
        }
        if (realtime_command_pending_.exchange(true)) {
            puts("aec_realtime rejected: command pending");
            return;
        }
        const auto generation = codec_.CaptureGeneration();
        Application::GetInstance().Schedule([this, generation] {
            if (codec_.RealtimeEligible() && codec_.CaptureGeneration() == generation) {
                Application::GetInstance().SetAecMode(kAecOnDeviceSide);
                puts("aec_realtime on: operator choice; acoustic acceptance unverified");
            } else
                puts("aec_realtime rejected: stale generation");
            realtime_command_pending_ = false;
        });
        return;
    }
    family_robot::DiagnosticRequest request{};
    if (family_robot::ParseDiagnosticRequest(line, request)) {
        StopCapture();
        std::lock_guard<std::mutex> destination_lock(destination_mutex_);
        portENTER_CRITICAL(&mux_);
        destination_ = request;
        until_ = esp_timer_get_time() + int64_t(request.seconds) * 1000000;
        ++window_;
        sequences_[0] = sequences_[1] = 0;
        positions_[0] = positions_[1] = 0;
        generations_[0] = generations_[1] = 0;
        enabled_ = true;
        discontinuity_[0] = discontinuity_[1] = true;
        portEXIT_CRITICAL(&mux_);
        puts("aec_diag started");
        return;
    }
    puts("aec command rejected");
}
void AecDiagnostics::Console() {
    // This variant makes USB Serial/JTAG the PRIMARY console; stock UART remains
    // unchanged. Bounded line reader never runs in ISR, main, or audio tasks.
    char line[96]{};
    size_t used = 0;
    bool overflow = false;
    // IDF fff9895's polling USB backend returns immediately without RX data.
    // O_NONBLOCK skips its FIFO prefetch, then queries the optional interrupt
    // driver's byte count (zero when not installed), making every read empty.
    // Keep the VFS flag clear; the underlying polling RX remains nonblocking.
    fcntl(STDIN_FILENO, F_SETFL, 0);
    while (running_) {
        char c;
        const auto n = read(STDIN_FILENO, &c, 1);
        if (n != 1) {
            vTaskDelay(pdMS_TO_TICKS(20));
            continue;
        }
        if (c == '\r' || c == '\n') {
            if (overflow)
                puts("aec command rejected: line too long");
            else if (used) {
                line[used] = '\0';
                Command(line);
            }
            used = 0;
            overflow = false;
        } else if (c >= 32 && c <= 126) {
            if (used + 1 < sizeof(line))
                line[used++] = c;
            else
                overflow = true;
        } else
            overflow = true;
    }
    vTaskDelete(nullptr);
}
#endif

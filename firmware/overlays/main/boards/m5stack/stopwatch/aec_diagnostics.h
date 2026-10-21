#pragma once
#include "sdkconfig.h"
#if CONFIG_STOPWATCH_SOFTWARE_AEC
#include <freertos/FreeRTOS.h>
#include <freertos/queue.h>
#include <freertos/task.h>
#include <atomic>
#include <mutex>
#include "aec_diagnostic_wire.h"
class SoftwareAecCodec;
class AecDiagnostics {
public:
    explicit AecDiagnostics(SoftwareAecCodec& codec) : codec_(codec) {}
    bool Start();
    void ResetStream(uint8_t stream);
    void Record(uint8_t stream, const int16_t* pcm, size_t frames, uint8_t channels,
                uint32_t generation, bool valid);

private:
    struct Packet {
        uint32_t window;
        uint16_t length;
        uint8_t bytes[1200];
    };
    SoftwareAecCodec& codec_;
    portMUX_TYPE mux_ = portMUX_INITIALIZER_UNLOCKED;
    std::mutex destination_mutex_;
    QueueHandle_t queue_ = nullptr;
    StaticQueue_t queue_control_{};
    uint8_t* queue_storage_ = nullptr;
    TaskHandle_t worker_ = nullptr, console_ = nullptr;
    std::atomic<bool> running_{false}, realtime_command_pending_{false};
    std::atomic<uint32_t> drops_{0};
    family_robot::DiagnosticRequest destination_{};
    int64_t until_ = 0;
    uint32_t window_ = 0, sequences_[2]{}, generations_[2]{};
    uint64_t positions_[2]{};
    bool enabled_ = false;
    bool discontinuity_[2]{true, true};
    void Worker();
    void Console();
    void Command(const char* line);
    void StopCapture();
};
#endif

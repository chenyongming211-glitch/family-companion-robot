#pragma once
#include <functional>
#include <string>
namespace FamilyOnboarding {
// Presence includes malformed identities: those must fail closed, never use legacy cloud.
bool Present();
bool Activated();
std::string Key();
void Start(std::function<void(bool)> connection);
void Configure();
// Called by the shared application before codec/audio initialization.
bool PrepareVoiceOnBoot();
// Existing heap telemetry replays startup once; cleanup facts persist across early USB loss.
void ReportConfigureStartup();
constexpr const char* Bootstrap = "https://robot.songguoxue.com/api/device/bootstrap";
constexpr const char* Voice = "wss://robot.songguoxue.com/xiaozhi/v1/";
}

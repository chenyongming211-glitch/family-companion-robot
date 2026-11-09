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
constexpr const char* Bootstrap = "https://robot.songguoxue.com/api/device/bootstrap";
constexpr const char* Voice = "wss://robot.songguoxue.com/xiaozhi/v1/";
}

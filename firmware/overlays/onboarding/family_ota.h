#pragma once
#include <cstdint>
#include <string>
namespace FamilyOta {
constexpr const char* Endpoint="https://robot.songguoxue.com/api/device/firmware";
void InitializeBoot(); // application owner, after AudioService::Start
void Tick(); // existing application 1 Hz tick
bool Reserved();
bool Ready();
std::string Report(const std::string& boot,uint64_t seq,uint64_t* revision=nullptr);
void Receive(const std::string& body,uint64_t received_at,uint64_t revision=0);
}

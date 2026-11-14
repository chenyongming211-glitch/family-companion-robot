#pragma once
#include <cstdio>
#include <cstdlib>
#include <ctime>

namespace family_robot {
// The system clock stays UTC for both OTA and SNTP. TZ affects presentation only.
inline bool SetDeviceTimezoneOffset(double minutes) {
    // This positive range check also rejects NaN/infinity before integer conversion.
    if (!(minutes >= -840 && minutes <= 840)) return false;
    const int offset = static_cast<int>(minutes);
    if (minutes != offset) return false;
    const int magnitude = offset < 0 ? -offset : offset;
    char zone[20];
    // POSIX TZ uses the opposite sign to the server's minutes east of UTC.
    std::snprintf(zone, sizeof(zone), "UTC%c%d:%02d", offset < 0 ? '+' : '-',
                  magnitude / 60, magnitude % 60);
    if (setenv("TZ", zone, 1) != 0) return false;
    tzset();
    return true;
}
inline void ConfigureDefaultDeviceTimezone() { SetDeviceTimezoneOffset(480); }
} // namespace family_robot

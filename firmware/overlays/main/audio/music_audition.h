#pragma once
#include "sdkconfig.h"
#if CONFIG_FAMILY_MUSIC_AUDITION
namespace MusicAudition {
void Start();
bool Active();
bool Cancelled();
void Cancel();
}
#endif

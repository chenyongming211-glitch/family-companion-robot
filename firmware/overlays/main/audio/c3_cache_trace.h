#pragma once
#include "c3_cache_span.h"
// Only the native PCM adapter links these. Kept out of legacy configurations.
void family_c3_cache_enable();
bool family_c3_cache_observe(bool rx);
bool family_c3_cache_read(unsigned,family_robot::C3CacheSpan::Record&);
bool family_c3_cache_peak(family_robot::C3CacheSpan::Record&);
bool family_c3_cache_overflowed();
unsigned family_c3_cache_slots_used();
void family_c3_cache_report();

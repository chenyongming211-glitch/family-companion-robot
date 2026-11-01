#include "sdkconfig.h"
#if CONFIG_XMINI_C3_PCM
#include "family_c3_codec.h"
#include "c3_cache_trace.h"
#include <esp_attr.h>
#include <esp_timer.h>
#include <algorithm>
#include <cstring>

// This object is noflash. The linker fragment also maps the task object's
// shared COMDAT helpers by exact symbol. Final ELF closure auditing rejects
// any Flash winner, clone, literal or unknown indirect call.
static DRAM_ATTR family_robot::C3CacheSpan family_c3_cache_trace;
void family_c3_cache_enable() {family_c3_cache_trace.Enable();}
bool family_c3_cache_observe(bool rx) {return family_c3_cache_trace.Observe(rx);}
bool family_c3_cache_read(unsigned i,family_robot::C3CacheSpan::Record& out) {return family_c3_cache_trace.Read(i,out);}
bool family_c3_cache_peak(family_robot::C3CacheSpan::Record& out) {return family_c3_cache_trace.ReadPeak(out);}
bool family_c3_cache_overflowed() {return family_c3_cache_trace.overflowed();}
unsigned family_c3_cache_slots_used() {return family_c3_cache_trace.used();}

#include "family_c3_flash_wrap.inc"
#include "family_c3_isr.inc"
#endif

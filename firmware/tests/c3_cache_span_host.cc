#include "c3_cache_span.h"
#include <cassert>
#include <cstdint>
#include <iostream>
#include <limits>
#include <functional>
#include <thread>
using family_robot::C3CacheSpan;
namespace family_robot {
struct C3CacheSpanTestPeer {
    static void Busy(C3CacheSpan& trace) {trace.Store(trace.slots_[0].seq,9);trace.Store(trace.active_,0);}
    static void Tear(C3CacheSpan& trace) {trace.Store(trace.slots_[0].disabled.high,0xffffffff);}
    static void ExhaustGeneration(C3CacheSpan& trace) {trace.Store(trace.used_,UINT32_MAX);}
    static void PublishWide(C3CacheSpan& trace,int64_t value) {
        auto& slot=trace.slots_[0];trace.WriteBegin(slot);
        trace.StoreTime(slot.enter,value);trace.StoreTime(slot.disabled,value);
        trace.StoreTime(slot.before_enabled,value+1);trace.StoreTime(slot.after_enabled,value+2);
        trace.WriteEnd(slot);
    }
};
}
static C3CacheSpan family_c3_cache_trace;
static int64_t clock_us=0;
static unsigned timer_calls=0,disable_calls=0,enable_calls=0;
static std::function<void()> enable_hook;
static int64_t esp_timer_get_time() {++timer_calls;return ++clock_us;}
extern "C" void __real_spi_flash_disable_interrupts_caches_and_other_cpu() {++disable_calls;}
extern "C" void __real_spi_flash_enable_interrupts_caches_and_other_cpu() {++enable_calls;if(enable_hook)enable_hook();}
#include "../overlays/main/boards/xmini/c3/family_c3_flash_wrap.inc"
static void Span(C3CacheSpan& trace, int64_t start=100) {
    auto token=trace.EnterBefore(start);
    trace.DisableAfter(token,start+1);
    assert(trace.Observe(true));assert(trace.Observe(false));
    trace.EnableBefore(token,start+50);
    assert(!trace.Observe(true));
    trace.EnableAfter(token,start+51);
}
int main() {
    C3CacheSpan trace;
    assert(!trace.enabled());assert(!trace.EnterBefore(1).valid());
    assert(!trace.Observe(true));
    trace.Enable();Span(trace);
    C3CacheSpan::Record record;
    assert(trace.Read(0,record));
    assert(record.enter_before==100 && record.disable_after==101 && record.enable_before==150 && record.enable_after==151);
    assert(record.rx==1 && record.tx==1 && record.proven);
    // Busy/changed sequence must return immediately; a torn RV32 timer is never evidence.
    auto pending=trace.EnterBefore(200);
    trace.DisableAfter(pending,201);
    assert(!trace.Read(1,record));
    trace.EnableBefore(pending,250);
    auto later=trace.EnterBefore(300);trace.DisableAfter(later,301);
    trace.EnableBefore(later,350);trace.EnableAfter(later,351);
    trace.EnableAfter(pending,999); // scheduler delay finishes the ORIGINAL slot
    assert(trace.Read(1,record) && record.enable_after==999 && record.disable_after==201);
    assert(trace.Read(2,record) && record.enable_after==351 && record.disable_after==301);
    auto nested=trace.EnterBefore(400);trace.DisableAfter(nested,401);
    auto inner=trace.EnterBefore(402);trace.DisableAfter(inner,403);assert(!inner.valid());
    trace.EnableBefore(nested,450);trace.EnableAfter(nested,451);
    assert(trace.Read(3,record) && !record.proven);
    for(unsigned i=4;i<C3CacheSpan::kSlots;++i)Span(trace,500+i*100);
    assert(trace.EnterBefore(9999).valid()); // completed short slots are reusable
    C3CacheSpan delayed;delayed.Enable();
    C3CacheSpan::Token in_flight[C3CacheSpan::kSlots];
    for(unsigned i=0;i<C3CacheSpan::kSlots;++i) {
        in_flight[i]=delayed.EnterBefore(1000+i*100);assert(in_flight[i].valid());
        delayed.DisableAfter(in_flight[i],1001+i*100);
        assert(delayed.BeginEnable(1050+i*100).valid()); // after point delayed by scheduler
    }
    auto excess=delayed.EnterBefore(9999);delayed.DisableAfter(excess,10000);
    assert(!excess.valid() && delayed.overflowed());
    C3CacheSpan peak;peak.Enable();auto longest=peak.EnterBefore(100000);
    peak.DisableAfter(longest,100001);assert(peak.Observe(true));assert(peak.Observe(false));
    peak.EnableBefore(longest,200000);peak.EnableAfter(longest,200001);
    for(unsigned i=0;i<4096;++i)Span(peak,300000+i*100);
    assert(peak.ReadPeak(record) && record.proven && record.generation==longest.generation);
    assert(record.enter_before==100000 && record.disable_after==100001 && record.enable_before==200000 && record.enable_after==200001);
    assert(record.rx==1 && record.tx==1 && !peak.overflowed());
    C3CacheSpan invalid;invalid.Enable();
    auto token=invalid.EnterBefore(INT64_MAX);invalid.DisableAfter(token,-1);
    assert(!invalid.Observe(true));invalid.EnableBefore(token,2);invalid.EnableAfter(token,3);
    assert(invalid.Read(0,record) && !record.proven);
    family_robot::C3CacheSpanState<2,2> saturated;saturated.Enable();
    auto limit=saturated.EnterBefore(1);saturated.DisableAfter(limit,2);
    assert(saturated.Observe(true));assert(saturated.Observe(true));assert(!saturated.Observe(true));
    saturated.EnableBefore(limit,3);saturated.EnableAfter(limit,4);
    decltype(saturated)::Record limited;assert(saturated.Read(0,limited) && limited.rx==2 && !limited.proven);
    C3CacheSpan missing;missing.Enable();auto absent=missing.EnterBefore(1);
    missing.EnableBefore(absent,3);missing.EnableAfter(absent,4);
    assert(!missing.Read(0,record) && missing.overflowed());
    C3CacheSpan preempted;preempted.Enable();auto before_preemption=preempted.EnterBefore(100);
    Span(preempted,200); // complete other-task operation before our real guard
    preempted.DisableAfter(before_preemption,500);preempted.EnableBefore(before_preemption,550);preempted.EnableAfter(before_preemption,551);
    assert(preempted.Read(1,record) && record.proven && record.enter_before==100 && record.disable_after==500 && record.enable_after==551);
    C3CacheSpan torn;torn.Enable();Span(torn);
    family_robot::C3CacheSpanTestPeer::Busy(torn);family_robot::C3CacheSpanTestPeer::Tear(torn);
    assert(!torn.Read(0,record));assert(!torn.Observe(true));
    C3CacheSpan exhausted;exhausted.Enable();family_robot::C3CacheSpanTestPeer::ExhaustGeneration(exhausted);
    auto full=exhausted.EnterBefore(1);exhausted.DisableAfter(full,2);
    assert(!full.valid() && exhausted.overflowed());
    C3CacheSpan racing;racing.Enable();Span(racing);
    family_robot::C3CacheSpanTestPeer::PublishWide(racing,0x1234567800000000LL);
    std::thread writer([&] {
        for(unsigned i=0;i<20000;++i)family_robot::C3CacheSpanTestPeer::PublishWide(racing,
            i%2?0x1234567800000000LL:0x23456789ffffffffLL);
    });
    for(unsigned i=0;i<20000;++i)if(racing.Read(0,record)) {
        assert(record.disable_after==record.enter_before);
        assert(record.enable_before==record.enter_before+1 && record.enable_after==record.enter_before+2);
    }
    writer.join();
    // Execute the SAME wrappers as production, including the disabled boot path.
    __wrap_spi_flash_disable_interrupts_caches_and_other_cpu();
    __wrap_spi_flash_enable_interrupts_caches_and_other_cpu();
    assert(disable_calls==1 && enable_calls==1 && timer_calls==0);
    family_c3_cache_trace.Enable();
    __wrap_spi_flash_disable_interrupts_caches_and_other_cpu();
    assert(family_c3_cache_trace.Observe(true));
    enable_hook=[] {
        enable_hook=nullptr;
        __wrap_spi_flash_disable_interrupts_caches_and_other_cpu();
        assert(family_c3_cache_trace.Observe(false));
        __wrap_spi_flash_enable_interrupts_caches_and_other_cpu();
    };
    __wrap_spi_flash_enable_interrupts_caches_and_other_cpu();
    assert(disable_calls==3 && enable_calls==3 && timer_calls==8);
    assert(family_c3_cache_trace.Read(0,record) && record.rx==1 && record.proven && record.enable_after==8);
    assert(family_c3_cache_trace.Read(1,record) && record.tx==1 && record.proven && record.enable_after==7);
    __wrap_spi_flash_disable_interrupts_caches_and_other_cpu();
    __wrap_spi_flash_disable_interrupts_caches_and_other_cpu();
    __wrap_spi_flash_enable_interrupts_caches_and_other_cpu();
    __wrap_spi_flash_enable_interrupts_caches_and_other_cpu();
    assert(disable_calls==5 && enable_calls==5 && family_c3_cache_trace.Read(2,record) && !record.proven);
    std::cout << "cache span: passthrough, four points, RX/TX, busy, delayed completion, nesting, budget, timestamp overflow passed\n";
}

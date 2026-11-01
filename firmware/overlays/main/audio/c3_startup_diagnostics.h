#pragma once
#include <cstdint>
namespace family_robot {
// Numeric evidence only. None/NotEntered and INT32_MIN mean unobserved.
enum class C3StartupFaultSource:uint32_t {
    None,Setup,OutputGeneration,CaptureGeneration,HardwareEpoch,EpochArm,
    ClosedDescriptors,SilenceTimeout,PrepareHardware,OutputHardware,Read,Write,
    CaptureBoundary,ReferenceDeadline,ReferenceTake,ExplicitReferenceFailure,
    TxShape,TxDescriptors,TxPair,TxPlayback,TxScrub,RxPair,
    PlaybackConnection,PlaybackDeadline,OutputTimeout
};
struct C3HardwareCheck {
    // Bits 0/1 read TX/RX; 2..11 dir/mode/pair/enabled/config TX/RX;
    // 12..25 DMA,bclk,mclk,rate,data width,slot width,mono,left mask,
    // ws width,ws polarity,bit shift,left align,big endian,lsb order.
    uint32_t mask=0,channel=0;
    int32_t tx_result=INT32_MIN,rx_result=INT32_MIN;
    // DMA,bclk,mclk,rate,data width,slot width,mode,mask,ws width,
    // ws polarity,bit shift,left align,big endian,lsb order.
    uint32_t actual[14]{};
    uint32_t state=0; // TX/RX dir[0:3]/[4:7],mode[8:11]/[12:15],pair/enabled/config bits16..21
};
struct C3CodecStartupDiagnostics {
    C3StartupFaultSource source=C3StartupFaultSource::None;
    uint32_t pair_fault=UINT32_MAX;
    int64_t timestamp_us=0,tx_us=0,rx_us=0,tx_samples=0,rx_samples=0,callback_previous_us=0;
    uint32_t capture_active=0,callbacks_armed=0,hardware_open=0;
    C3HardwareCheck hardware{};
};
enum class C3StartupStep:uint32_t {NotEntered,Entered,Succeeded,Failed};
enum class C3StartupStage:uint32_t {
    Initialize,CodecStarted,PackComplete,InputSrcComplete,AecComplete,InitializeComplete,
    Start,InputTaskComplete,OutputTaskComplete,StartComplete
};
struct C3StartupHash {
    C3StartupStep state=C3StartupStep::NotEntered;
    int32_t init_status=INT32_MIN,hash_status=INT32_MIN;
    uint32_t length=UINT32_MAX,matched=UINT32_MAX;
};
struct C3StartupHeap {
    C3StartupStage stage=C3StartupStage::Initialize;
    int64_t timestamp_us=0;
    uint32_t free=0,largest=0,minimum=0;
};
struct C3StartupDiagnostics {
    C3StartupStep pack_partition=C3StartupStep::NotEntered,map=C3StartupStep::NotEntered,
        pack_open=C3StartupStep::NotEntered,input_src=C3StartupStep::NotEntered,
        audio_engine=C3StartupStep::NotEntered,aec_create=C3StartupStep::NotEntered,
        input_task=C3StartupStep::NotEntered,output_task=C3StartupStep::NotEntered;
    int32_t map_result=INT32_MIN,input_src_result=INT32_MIN,
        input_task_result=INT32_MIN,output_task_result=INT32_MIN;
    uint32_t partition_address=0,partition_size=0,pack_count=0,hash_count=0;
    C3StartupHash hash[2]{};
    static constexpr unsigned kHeapStages=10;
    uint32_t heap_count=0;
    C3StartupHeap heap[kHeapStages]{};
};
static_assert(sizeof(C3StartupDiagnostics)<=448,"bounded startup metadata");
static_assert(sizeof(C3CodecStartupDiagnostics)<=160,"bounded first-fault metadata");
}

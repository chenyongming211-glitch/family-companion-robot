// SDK plumbing prepended from the actual codec harness.
#include <type_traits>
#include "family_c3_codec.h"
#include "family_c3_lifecycle.inc"
#include "family_c3_playback.inc"
#include "family_c3_dma.inc"
#include "family_c3_capture.inc"
template<class C,class=void>struct HasStartupDiagnostics:std::false_type{};
template<class C>struct HasStartupDiagnostics<C,std::void_t<decltype(std::declval<C>().StartupDiagnostics())>>:std::true_type{};
template<class C>static void CheckDiagnostics(){
 if constexpr(!HasStartupDiagnostics<C>::value)assert(!"PCM first-fault diagnostics are missing");
 else {
  using Source=typename C::StartupFaultSource;
  {
   C codec(nullptr,0,24000,24000,0,0,0,0,0,0,1);
   auto empty=codec.StartupDiagnostics();assert(empty.source==Source::None&&empty.timestamp_us==0);
   codec.tx_config.slot_cfg.ws_width=32;codec.Start();assert(codec.ReferenceFaulted());
   auto first=codec.StartupDiagnostics();assert(first.source==Source::HardwareEpoch&&first.timestamp_us==now_us);
   assert(first.hardware.mask==(1u<<20)&&first.hardware.channel==1&&first.hardware.actual[8]==32);
   assert(first.hardware.actual[0]==2880&&first.hardware.actual[1]==768000&&first.hardware.actual[2]==6144000);
   now_us+=12345;codec.ReportReferenceFailure();auto later=codec.StartupDiagnostics();
   assert(std::memcmp(&first,&later,sizeof(first))==0);
  }
  {
   C codec(nullptr,0,24000,24000,0,0,0,0,0,0,1);codec.rx.info_fails=true;codec.Start();
   auto d=codec.StartupDiagnostics();assert(d.source==Source::HardwareEpoch&&d.hardware.mask==2&&d.hardware.rx_result==-1);
   assert(d.hardware.tx_result==ESP_OK);
   codec.rx.info_fails=false;
  }
  {
   C codec(nullptr,0,24000,24000,0,0,0,0,0,0,1);codec.tx.info_fails=true;codec.Start();
   auto d=codec.StartupDiagnostics();assert(d.hardware.mask==1&&d.hardware.rx_result==INT32_MIN);
   codec.tx.info_fails=false;
  }
  for(unsigned bit=12;bit<=25;++bit){
   C codec(nullptr,0,24000,24000,0,0,0,0,0,0,1);auto& slot=codec.tx_config.slot_cfg;
   switch(bit){case 12:codec.tx.info.total_dma_buf_size=1;break;case 13:codec.tx.info.bclk_hz=1;break;
    case 14:codec.tx.info.mclk_hz=1;break;case 15:codec.tx_config.clk_cfg.sample_rate_hz=1;break;
    case 16:slot.data_bit_width=8;break;case 17:slot.slot_bit_width=8;break;case 18:slot.slot_mode=0;break;
    case 19:slot.slot_mask=0;break;case 20:slot.ws_width=32;break;case 21:slot.ws_pol=true;break;
    case 22:slot.bit_shift=false;break;case 23:slot.left_align=false;break;case 24:slot.big_endian=true;break;
    case 25:slot.bit_order_lsb=true;break;}
   codec.Start();auto d=codec.StartupDiagnostics();assert(d.source==Source::HardwareEpoch&&d.hardware.mask==(1u<<bit)&&d.hardware.channel==1);
  }
  for(unsigned bit=2;bit<=11;++bit){
   if(bit==8||bit==9)continue; // base-codec plumbing owns channel enable
   C codec(nullptr,0,24000,24000,0,0,0,0,0,0,1);
   switch(bit){case 2:codec.tx.info.dir=0;break;case 3:codec.rx.info.dir=0;break;
    case 4:codec.tx.info.mode=0;break;case 5:codec.rx.info.mode=0;break;
    case 6:codec.tx.info.pair_chan=nullptr;break;case 7:codec.rx.info.pair_chan=nullptr;break;
    case 10:codec.tx.info.mode_cfg=nullptr;break;case 11:codec.rx.info.mode_cfg=nullptr;break;}
   codec.Start();auto d=codec.StartupDiagnostics();assert(d.hardware.mask==(1u<<bit));
  }
  {
   C codec(nullptr,0,24000,24000,0,0,0,0,0,0,1);codec.Start();
   int16_t dma[6][240]{};unsigned descriptor=0;
   auto sent=[&]{i2s_event_data_t event{dma[descriptor++%6],480};codec.tx.callbacks.on_sent(&codec.tx,&event,codec.tx.argument);};
   now_us+=10000;sent();now_us+=10000;sent();
   auto allocations=allocation_calls;now_us+=20000;sent();
   auto first=codec.StartupDiagnostics();assert(first.source==Source::TxPair&&first.timestamp_us==now_us);
   assert(first.pair_fault==3&&first.tx_samples==240&&first.callbacks_armed&&first.hardware_open&&!first.capture_active);
   assert(first.tx_us==now_us&&allocation_calls==allocations);
   assert(first.callback_previous_us==now_us-20000);
   std::thread reader([&]{for(int n=0;n<1000;++n){auto d=codec.StartupDiagnostics();assert(std::memcmp(&first,&d,sizeof(d))==0);}});
   for(int n=0;n<1000;++n){now_us+=10000;sent();}reader.join();
  }
 }
}
int main(){CheckDiagnostics<FamilyC3Codec>();std::puts("PCM startup first-fault diagnostics passed");}

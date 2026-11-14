#define main voice_regression_main
#include "waveshare_codec_host.cc"
#undef main
int main(){
 {auto c=make_codec();c.Start();assert(!c.FirmwareHealthy());int16_t dma[6][240]{};for(int i=0;i<12;++i)tick(c,dma[i%6]);assert(!c.FirmwareHealthy());std::vector<int16_t> data(480);assert(c.InputData(data)&&c.FirmwareHealthy());now_us+=600000;assert(!c.FirmwareHealthy());assert(c.FirmwareAvailable());}
 {auto c=make_codec();c.Start();std::vector<int16_t> data(480);assert(c.InputData(data));int16_t dma[6][240]{};for(int i=0;i<12;++i)tick(c,dma[i%6]);assert(c.FirmwareHealthy());const auto old=c.CaptureGeneration();assert(c.PauseForFirmware());assert(c.muted&&!c.tx.enabled&&!c.rx.enabled&&!c.ReferenceValid()&&!c.FirmwareHealthy());
  const int changes=c.input_changes+c.output_changes;const auto raw=raw_reads;c.EnableInput(true);c.EnableOutput(true);c.EnableInput(false);c.EnableOutput(false);assert(!c.InputData(data)&&!c.PrepareAudioConnection()&&!c.BeginMusic(48000));assert(c.input_changes+c.output_changes==changes&&raw_reads==raw);
  now_us+=20000000;for(int i=0;i<4;++i)tick(c,dma[0]);assert(!c.ReferenceFaulted());size_t n=0;delay_hook=[&]{tick(c,dma[n++%6],480,0);assert(c.muted);};assert(c.ResumeAfterFirmware());delay_hook={};assert(n>=12&&c.muted&&!c.ReferenceFaulted()&&c.CaptureGeneration()!=old&&!c.FirmwareHealthy());
  const auto restored_generation=c.CaptureGeneration();const auto restored_changes=c.input_changes+c.output_changes;
  c.EnableInput(false);c.EnableOutput(false);c.EnableInput(true);c.EnableOutput(true);
  assert(!c.BeginMusic(48000)&&!c.PrepareAudioConnection()&&c.input_changes+c.output_changes==restored_changes&&c.CaptureGeneration()==restored_generation);
  assert(!c.FinishFirmwareResume());assert(c.InputData(data)&&c.FirmwareHealthy());assert(c.FinishFirmwareResume()&&!c.muted&&c.FirmwareAvailable());assert(raw_reads==raw&&"firmware restore must not add a competing RX reader");
  c.ReportReferenceFailure();int16_t recovered[6][240]{};n=0;delay_hook=[&]{tick(c,recovered[n++%6],480,0);};assert(c.PrepareAudioConnection()&&"OTA must not spend the once-per-boot fault recovery");delay_hook={};}
 for(int bad=0;bad<5;++bad){auto c=make_codec();c.Start();if(bad==0)c.mute_fails=true;if(bad==1)c.close_fails=true;assert(c.PauseForFirmware()==(bad>1));if(bad==0)c.mute_fails=false;if(bad==1)c.close_fails=false;int16_t dma[6][240]{};size_t n=0;delay_hook=[&]{tick(c,dma[n++%6],480,0);};if(bad==2)c.reopen_fails=true;if(bad==3)delay_hook={};if(bad==4)c.ReportReferenceFailure();bool result=c.ResumeAfterFirmware();delay_hook={};if(bad>=2)assert(!result&&c.muted);}
 {auto c=make_codec();c.Start();std::promise<void> entered,release;auto wait=release.get_future();box_read_hook=[&]{entered.set_value();wait.wait();};std::vector<int16_t> input(480);auto reader=std::async(std::launch::async,[&]{return c.InputData(input);});entered.get_future().wait();auto start=now_us;assert(!c.PauseForFirmware()&&now_us-start<=200000);release.set_value();assert(reader.get());box_read_hook={};}
 {auto c=make_codec();c.Start();assert(c.PauseForFirmware());
  int16_t dma[6][240]{};size_t n=0;delay_hook=[&]{tick(c,dma[n++%6],480,0);};assert(c.ResumeAfterFirmware());delay_hook={};
  std::vector<int16_t> input(480);assert(c.InputData(input)&&c.FirmwareHealthy());
  std::promise<void> entered,release;auto wait=release.get_future();box_read_hook=[&]{entered.set_value();wait.wait();};
  auto reader=std::async(std::launch::async,[&]{return c.InputData(input);});entered.get_future().wait();now_us+=5000;
  const bool healthy=c.FirmwareHealthy();const bool finished=c.FinishFirmwareResume();
  release.set_value();assert(reader.get());box_read_hook={};
  std::fprintf(stderr,"ordinary concurrent input: healthy=%d finished=%d\n",healthy,finished);
  assert(healthy&&finished&&!c.muted&&!c.ReferenceFaulted()&&"normal input I/O must not starve OTA health or PA release");
 }
 for(int race=0;race<4;++race){auto c=make_codec();c.Start();assert(c.PauseForFirmware());
  int16_t dma[6][240]{};size_t n=0;delay_hook=[&]{tick(c,dma[n++%6],480,0);};assert(c.ResumeAfterFirmware());delay_hook={};
  std::vector<int16_t> input(480);assert(c.InputData(input)&&c.FirmwareHealthy());
  unmute_hook=[&]{if(race==0)c.ReportReferenceFailure();if(race==1)c.CaptureBoundary();if(race==2)now_us+=12000;};
  if(race==3)c.unmute_fails=true;
  assert(!c.FinishFirmwareResume()&&c.muted&&c.ReferenceFaulted()&&!c.FirmwareAvailable());unmute_hook={};
 }
}

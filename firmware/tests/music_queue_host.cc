#include <atomic>
#include <cassert>
#include <chrono>
#include <condition_variable>
#include <deque>
#include <future>
#include <memory>
#include <mutex>
#include <thread>
namespace family_robot {struct PlaybackIdentity{unsigned connection=1;};}
namespace MusicAudition {
std::atomic<bool> active{false};std::atomic<unsigned> reads{0};
bool Active(){++reads;return active.load();}
}
struct AudioStreamPacket{family_robot::PlaybackIdentity receipt_id;unsigned output_generation=0;};
struct AudioCodec{
 enum class PlaybackOp{Invalidate,Release};
 unsigned invalidated=0,released=0;
 void PlaybackControl(PlaybackOp op,family_robot::PlaybackIdentity){if(op==PlaybackOp::Release)++released;else ++invalidated;}
 unsigned PlaybackGeneration(){return 1;}
};
struct AudioService{
 static constexpr unsigned MAX_DECODE_PACKETS_IN_QUEUE=2;
 AudioCodec device;AudioCodec* codec_=&device;
 std::mutex audio_queue_mutex_;std::condition_variable audio_queue_cv_;
 std::deque<std::unique_ptr<AudioStreamPacket>> audio_decode_queue_;
 std::atomic<bool> service_stopped_{false};unsigned playback_generation_=1;
 bool playback_drained_notified_=true;
 bool PushPacketToDecodeQueue(std::unique_ptr<AudioStreamPacket>,bool);
};
// PRODUCTION_QUEUE_METHOD
int main(){
 using namespace std::chrono_literals;
 {
  AudioService a;MusicAudition::active=false;MusicAudition::reads=0;
  std::unique_lock<std::mutex> held(a.audio_queue_mutex_);
  std::atomic<bool> started=false;
  auto producer=std::async(std::launch::async,[&]{started=true;return a.PushPacketToDecodeQueue(std::make_unique<AudioStreamPacket>(),true);});
  while(!started)std::this_thread::yield();
  // A pre-lock check may run, but cannot authorize later queue admission.
  const auto end=std::chrono::steady_clock::now()+100ms;
  while(!MusicAudition::reads && std::chrono::steady_clock::now()<end)std::this_thread::yield();
  MusicAudition::active=true;held.unlock();
  assert(!producer.get()&&a.audio_decode_queue_.empty());
  assert(a.device.released==1&&a.device.invalidated==1);
 }
 {
  AudioService a;MusicAudition::active=false;
  for(int i=0;i<2;++i)a.audio_decode_queue_.push_back(std::make_unique<AudioStreamPacket>());
  auto producer=std::async(std::launch::async,[&]{return a.PushPacketToDecodeQueue(std::make_unique<AudioStreamPacket>(),true);});
  assert(producer.wait_for(100ms)==std::future_status::timeout);
  {std::lock_guard<std::mutex> held(a.audio_queue_mutex_);MusicAudition::active=true;a.audio_decode_queue_.clear();}
  a.audio_queue_cv_.notify_all();
  assert(producer.wait_for(1s)==std::future_status::ready);
  assert(!producer.get()&&a.audio_decode_queue_.empty()&&a.device.released==1);
 }
 {AudioService a;MusicAudition::active=false;
  assert(a.PushPacketToDecodeQueue(std::make_unique<AudioStreamPacket>(),true));
  assert(a.audio_decode_queue_.size()==1&&a.device.released==0);}
}

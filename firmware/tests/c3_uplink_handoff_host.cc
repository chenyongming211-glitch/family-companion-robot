// Exercise the production C3 AEC-to-Opus handoff; only SDK plumbing is fake.
#include <atomic>
#include <cassert>
#include <cstdint>
#include <cstring>
#include <functional>
#include <mutex>
#include <vector>
#include "c3_lite_aec.h"

struct AudioCodec {
    uint32_t generation=7,ready_generation=0;
    bool fault=false,valid=true;
    uint32_t CaptureGeneration()const{return generation;}
    bool ReferenceValid()const{return valid;}
    bool ReferenceFaulted()const{return fault;}
    void NotifyAecReady(bool ready,uint32_t gen){ready_generation=ready?gen:0;}
    void ReportReferenceFailure(){fault=true;}
};
class LiteAudioEngine {
public:
    AudioCodec* codec_=nullptr;
    std::atomic<bool> voice_processing_enabled_{true};
    std::atomic<uint32_t> capture_epoch_{0};
    uint32_t buffer_epoch_=0,c3_capture_generation_=0;
    family_robot::C3LiteAec* c3_aec_=family_robot::C3LiteAec::Create();
    int frame_samples_=960;
    std::vector<int16_t> output_buffer_;
    std::mutex output_mutex_;
    std::function<void(std::vector<int16_t>&&)> output_callback_;
    LiteAudioEngine(){output_buffer_.reserve(960);}
    ~LiteAudioEngine(){family_robot::C3LiteAec::Destroy(c3_aec_);}
    void FeedCaptured(std::vector<int16_t>&&,uint32_t);
    void OutputRawAudio(const std::vector<int16_t>&);
};
#include "c3_lite_uplink.inc"

static void feed(LiteAudioEngine& engine,size_t frames,uint32_t epoch,int16_t value) {
    std::vector<int16_t> mr(frames*2,1);
    for(size_t i=0;i<frames;++i)mr[2*i]=value+1;
    engine.FeedCaptured(std::move(mr),epoch);
}

int main(int argc,char** argv) {
    assert(argc==2);AudioCodec codec;LiteAudioEngine engine;engine.codec_=&codec;
    if(std::strcmp(argv[1],"bounded")==0) {
        size_t emitted=0;
        engine.output_callback_=[&](std::vector<int16_t>&& pcm){
            assert(pcm.size()==960);emitted+=pcm.size();
        };
        feed(engine,512,3,10);feed(engine,512,3,10);
        assert(engine.output_buffer_.capacity()<=960 && "working PCM storage must never grow beyond one Opus frame");
        assert(emitted==960 && engine.output_buffer_.size()==64);
    } else if(std::strcmp(argv[1],"ownership")==0) {
        std::vector<std::vector<int16_t>> queued;queued.reserve(2);
        int16_t* storage=nullptr;
        engine.output_callback_=[&](std::vector<int16_t>&& pcm){
            assert(&pcm==&engine.output_buffer_ && "handoff must forward its working vector, not construct a temporary copy");
            if(queued.empty())assert(pcm.data()==storage && "handoff must transfer the existing PCM allocation rather than copy it");
            else assert(pcm.data()!=storage && "queued frame storage must remain exclusively owned");
            queued.push_back(std::move(pcm));
        };
        feed(engine,512,3,10);storage=engine.output_buffer_.data();
        feed(engine,512,3,10);
        assert(queued.size()==1 && queued[0].data()==storage);
        feed(engine,1024,3,11);assert(queued.size()==2);
        for(auto value:queued[0])assert(value==10);
    } else if(std::strcmp(argv[1],"sequence")==0) {
        std::vector<std::vector<int16_t>> queued;queued.reserve(16);
        engine.output_callback_=[&](std::vector<int16_t>&& pcm){
            assert(pcm.size()==960);queued.push_back(std::move(pcm));
        };
        size_t total=0;
        for(size_t round=0;round<5;++round)for(size_t frames:{1u,159u,320u,511u,17u,512u,1024u,3u,77u}) {
            std::vector<int16_t> mr(frames*2,1);
            for(size_t i=0;i<frames;++i)mr[2*i]=static_cast<int16_t>(total+i+1);
            engine.FeedCaptured(std::move(mr),3);total+=frames;
            assert(engine.output_buffer_.capacity()<=960);
            const auto processed=total/512*512;
            assert(queued.size()==processed/960 && engine.output_buffer_.size()==processed%960);
        }
        size_t index=0;
        for(const auto& pcm:queued)for(auto value:pcm)assert(value==static_cast<int16_t>(index++));
        for(auto value:engine.output_buffer_)assert(value==static_cast<int16_t>(index++));
        assert(index==total/512*512);
    } else if(std::strcmp(argv[1],"epoch")==0) {
        std::vector<std::vector<int16_t>> queued;queued.reserve(3);
        engine.output_callback_=[&](std::vector<int16_t>&& pcm){queued.push_back(std::move(pcm));};
        feed(engine,512,3,10);assert(queued.empty());
        feed(engine,1024,5,20);
        assert(queued.size()==1);for(auto value:queued.back())assert(value==20);
        ++codec.generation;feed(engine,1024,5,30);
        assert(queued.size()==2);for(auto value:queued.back())assert(value==30);
        assert(engine.output_buffer_.size()==64 && engine.output_buffer_.capacity()<=960);
    } else if(std::strcmp(argv[1],"close")==0) {
        engine.frame_samples_=160;engine.output_buffer_=std::vector<int16_t>();engine.output_buffer_.reserve(160);
        size_t emitted=0;
        engine.output_callback_=[&](std::vector<int16_t>&& pcm){
            assert(pcm.size()==160);++emitted;
            codec.valid=false;++codec.generation;
        };
        feed(engine,512,3,10);
        assert(emitted==1 && "closing during handoff must stop all later frames from that AEC block");
        assert(engine.output_buffer_.empty());
    } else if(std::strcmp(argv[1],"invalid-frame")==0) {
        engine.frame_samples_=0;
        engine.output_callback_=[](std::vector<int16_t>&&){assert(false && "invalid frame size must fail before emission");};
        feed(engine,512,3,10);assert(codec.fault);
    } else assert(false && "unknown test case");
}

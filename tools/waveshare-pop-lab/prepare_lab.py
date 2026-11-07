"""Apply lab-only instrumentation to an already locked, fresh prepared tree."""
import argparse
import hashlib
import json
import pathlib
import shutil
from generate_fixture import generate

def prepare(tree, url, descriptors=6, yield_output_lock=False):
    tree=pathlib.Path(tree);here=pathlib.Path(__file__).resolve().parent
    changed={}
    def edit(name,old,new):
        p=tree/name;text=p.read_text()
        if old.startswith(' '):old='\n'+old;new='\n'+new
        if text.count(old)!=1:raise ValueError('lab anchor is not unique: '+name+' '+old[:60])
        changed.setdefault(name,{'before':hashlib.sha256(p.read_bytes()).hexdigest()})
        p.write_text(text.replace(old,new,1))
    def include(name):edit(name,'#include "audio_service.h"','#include "audio_service.h"\n#include "wave_pop_runtime.h"')
    generate(tree/'wave-pop-fixture',url)
    for src,dst in [('probe.h','wave_pop_probe.h'),('runtime.h','wave_pop_runtime.h'),('lab.inc','wave_pop_lab.inc')]:
        text=(here/src).read_text().replace('"probe.h"','"wave_pop_probe.h"')
        (tree/'main/audio'/dst).write_text(text)
    shutil.copyfile(tree/'wave-pop-fixture/fixture.h',tree/'main/audio/wave_pop_fixture.h')
    edit('main/audio/playback_progress.h','    bool HardwareHealthy() const { return hardware_ok_; }','    size_t WavePopQueueDepth() const { return used_; }\n    uint32_t WavePopCompletedCount(void* address) const { for(const auto& s:slots_)if(s.address==address)return s.tag.count; return 0; }\n    bool HardwareHealthy() const { return hardware_ok_; }')
    box='main/boards/waveshare/esp32-s3-touch-amoled-2.16/'
    edit(box+'xiaoman_box_codec.h','#include "experiment_gate.h"','#include "experiment_gate.h"\n#include "wave_pop_runtime.h"')
    edit(box+'xiaoman_box_codec.h','    void Start() override;','    wave_pop_lab::Probe WavePopProbe(bool reset) { portENTER_CRITICAL(&mux_); auto result=wave_pop_lab::probe; if(reset) wave_pop_lab::probe={}; portEXIT_CRITICAL(&mux_); return result; }\n    void Start() override;')
    edit(box+'xiaoman_box_dma.inc','        playback_->Complete(static_cast<int16_t*>(event->dma_buf),240,now);','        wave_pop_lab::probe.completed_samples+=playback_->WavePopCompletedCount(event->dma_buf);\n        const auto wave_pop_supply=wave_pop_lab::ReadSupplyISR();\n        wave_pop_lab::probe.Refill(wave_pop_supply.active,playback_->WavePopQueueDepth()!=0,wave_pop_supply.decode,wave_pop_supply.pcm);\n        playback_->Complete(static_cast<int16_t*>(event->dma_buf),240,now);')
    edit(box+'xiaoman_box_lifecycle.inc','    const bool read=BoxAudioCodec::InputData(data);','    const bool read=BoxAudioCodec::InputData(data);\n    if(read)wave_pop_lab::Input(data.data(),data.size());')
    edit('main/audio/audio_service.h','    void PrintC3Resources() const;','    void PrintC3Resources() const;\n    TaskHandle_t WavePopCodecTask() const { return opus_codec_task_handle_; }')
    edit('main/protocols/protocol.h','struct AudioStreamPacket {','struct AudioStreamPacket {\n    uint32_t wave_pop_queued_us=0;')
    include('main/audio/audio_service.cc')
    edit('main/audio/audio_service.cc','bool AudioService::PushPacketToDecodeQueue(std::unique_ptr<AudioStreamPacket> packet, bool wait) {','bool AudioService::PushPacketToDecodeQueue(std::unique_ptr<AudioStreamPacket> packet, bool wait) {\n    packet->wave_pop_queued_us=uint32_t(esp_timer_get_time());')
    edit('main/audio/audio_service.cc','    audio_decode_queue_.push_back(std::move(packet));','    audio_decode_queue_.push_back(std::move(packet));\n    wave_pop_lab::decode_backlog.store(audio_decode_queue_.size(),std::memory_order_relaxed);')
    edit('main/audio/audio_service.cc','            audio_decode_queue_.pop_front();','            audio_decode_queue_.pop_front();\n            wave_pop_lab::decode_backlog.store(audio_decode_queue_.size(),std::memory_order_relaxed);\n            wave_pop_lab::Max(wave_pop_lab::max_wait_us,uint32_t(esp_timer_get_time())-packet->wave_pop_queued_us);')
    edit('main/audio/audio_service.cc','            SetDecodeSampleRate(packet->sample_rate, packet->frame_duration);','            const auto wave_pop_decode_start=esp_timer_get_time();\n            SetDecodeSampleRate(packet->sample_rate, packet->frame_duration);')
    edit('main/audio/audio_service.cc','            decode_in_flight_ = true;','            decode_in_flight_ = true;\n            wave_pop_lab::decode_inflight.store(1,std::memory_order_relaxed);')
    edit('main/audio/audio_service.cc','            decode_in_flight_ = false;','            decode_in_flight_ = false;\n            wave_pop_lab::decode_inflight.store(0,std::memory_order_relaxed);')
    edit('main/audio/audio_service.cc','            if (decoded && generation == playback_generation_ && !service_stopped_.load()) {','            wave_pop_lab::Max(wave_pop_lab::max_decode_us,uint32_t(esp_timer_get_time()-wave_pop_decode_start));\n            if (decoded && generation == playback_generation_ && !service_stopped_.load()) {')
    edit('main/audio/audio_service.cc','                audio_playback_queue_.push_back(std::move(task));','                audio_playback_queue_.push_back(std::move(task));\n                wave_pop_lab::pcm_backlog.store(audio_playback_queue_.size(),std::memory_order_relaxed);')
    edit('main/audio/audio_service.cc','        audio_playback_queue_.pop_front();','        audio_playback_queue_.pop_front();\n        wave_pop_lab::pcm_backlog.store(audio_playback_queue_.size(),std::memory_order_relaxed);')
    edit('main/audio/audio_service.cc','        output_in_flight_ = true;','        output_in_flight_ = true;\n        wave_pop_lab::pcm_inflight.store(1,std::memory_order_relaxed);')
    edit('main/audio/audio_service.cc','        output_in_flight_ = false;','        output_in_flight_ = false;\n        wave_pop_lab::pcm_inflight.store(0,std::memory_order_relaxed);')
    edit('main/application.cc','#include "websocket_protocol.h"','#include "websocket_protocol.h"\n#include "wave_pop_lab.inc"')
    edit('main/application.cc','    audio_service_.Start();\n    ESP_LOGI(TAG, "After board/audio init");','    audio_service_.Start();\n    wave_pop_lab::Start(this,&audio_service_);\n    ESP_LOGI(TAG, "After board/audio init");')
    playback_path=tree/(box+'xiaoman_box_playback.inc')
    yielding='        lock.unlock();\n        vTaskDelay(1);\n        lock.lock();' in playback_path.read_text()
    if yield_output_lock and not yielding:
        edit(box+'xiaoman_box_playback.inc','    std::lock_guard<std::mutex> lock(lifecycle_);','    std::unique_lock<std::mutex> lock(lifecycle_);')
        edit(box+'xiaoman_box_playback.inc','        vTaskDelay(1);','        lock.unlock();\n        vTaskDelay(1);\n        lock.lock();')
        yielding=True
    board=box+'esp32-s3-touch-amoled-2.16.cc'
    # Older product trees still use 300 seconds; current Waveshare product
    # firmware already disables idle power-off. Do not rewrite preferences.
    if 'new PowerSaveTimer(-1, 60, 300)' in (tree/board).read_text():
        edit(board,'new PowerSaveTimer(-1, 60, 300)','new PowerSaveTimer(-1, 60, -1)')
    elif (tree/board).read_text().count('new PowerSaveTimer(-1, 60, -1)') != 1:
        raise ValueError('unrecognized Waveshare idle power policy')
    if descriptors==12:
        edit('main/audio/audio_codec.h','#define AUDIO_CODEC_DMA_DESC_NUM 6','#define AUDIO_CODEC_DMA_DESC_NUM 12')
        edit('main/audio/playback_progress.h','kBlock = 240, kSlots = 6,','kBlock = 240, kSlots = 12,')
        edit('main/audio/playback_progress.h','sizeof(PlaybackProgress)<=7360','sizeof(PlaybackProgress)<=7552')
        edit(box+'xiaoman_box_lifecycle.inc','tx.total_dma_buf_size!=2880','tx.total_dma_buf_size!=5760')
        edit(box+'xiaoman_box_lifecycle.inc','rx.total_dma_buf_size!=5760','rx.total_dma_buf_size!=11520')
    for name,d in changed.items():d['after']=hashlib.sha256((tree/name).read_bytes()).hexdigest()
    manifest={'kind':'waveshare-pop-lab-only','dma_descriptors':descriptors,'yield_output_lock':yielding,'changed':changed,'inputs':{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in here.iterdir() if p.is_file()},'added':{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in (tree/'main/audio').glob('wave_pop*')}}
    (tree/'wave-pop-fixture/lab-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--tree',required=True);p.add_argument('--fixture-url',default='');p.add_argument('--descriptors',type=int,choices=(6,12),default=6);p.add_argument('--yield-output-lock',action='store_true');a=p.parse_args();prepare(a.tree,a.fixture_url,a.descriptors,a.yield_output_lock)

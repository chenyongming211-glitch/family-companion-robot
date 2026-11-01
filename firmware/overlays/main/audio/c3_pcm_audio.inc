// The trailer owns a separate mmap handle: generic Assets::UnApplyPartition
// cannot invalidate cue bytes while the output worker borrows the pack.
static bool C3PcmSha256(const uint8_t* bytes,size_t size,uint8_t* digest) {
    // IDF initializes PSA at startup; this public call is safely idempotent.
    if(psa_crypto_init()!=PSA_SUCCESS)return false;
    size_t length=0;
    return psa_hash_compute(PSA_ALG_SHA_256,bytes,size,digest,32,&length)==PSA_SUCCESS && length==32;
}
static bool C3PcmVerifyHash(const uint8_t* bytes,size_t size,const uint8_t* expected,void*) {
    uint8_t digest[32];return C3PcmSha256(bytes,size,digest) && !std::memcmp(digest,expected,32);
}
bool AudioService::InitializeC3CuePack() {
    if(c3_pcm_.pack.count())return true;
    const auto* partition=esp_partition_find_first(ESP_PARTITION_TYPE_DATA,ESP_PARTITION_SUBTYPE_ANY,"assets");
    constexpr size_t offset=0x50000,capacity=0x1b0000;
    if(!partition || partition->address!=0x600000 || partition->size<offset ||
       kFamilyC3PcmPackBytes>capacity || kFamilyC3PcmPackBytes>partition->size-offset ||
       uint64_t(partition->address)+offset+kFamilyC3PcmPackBytes>=0x800000)return false;
    const void* bytes=nullptr;
    if(esp_partition_mmap(partition,offset,kFamilyC3PcmPackBytes,ESP_PARTITION_MMAP_DATA,&bytes,&c3_cue_mmap_)!=ESP_OK)return false;
    if(!c3_pcm_.pack.Open(static_cast<const uint8_t*>(bytes),kFamilyC3PcmPackBytes,kFamilyC3PcmPackSha256,C3PcmVerifyHash,nullptr) ||
       c3_pcm_.pack.count()!=21) {
        esp_partition_munmap(c3_cue_mmap_);c3_cue_mmap_=0;return false;
    }
    return true;
}
// Actual PCM media methods. No Opus task, queued heap PCM, or prompt decoder.
void AudioService::WakeC3PcmOwner() {
    if(callbacks_.on_send_queue_available)callbacks_.on_send_queue_available();
}
void AudioService::RequestC3PcmFault(uint32_t epoch) {
    if(!epoch)epoch=button_uplink_.Revision();
    button_uplink_.Revoke(epoch); // never waits for synchronous network Send
    auto previous=c3_pcm_.fault_epoch.load();
    while(previous<epoch && !c3_pcm_.fault_epoch.compare_exchange_weak(previous,epoch)) {}
    c3_pcm_.fault_pending.store(true);
    WakeC3PcmOwner();
}
bool AudioService::FinishC3PcmFault() {
    if(!c3_pcm_.fault_pending.exchange(false))return false;
    const auto epoch=c3_pcm_.fault_epoch.load();
    const bool closed=button_uplink_.CloseRevoked(epoch,[this]{ClearC3UplinkQueues();});
    if(closed) {
        {std::lock_guard<std::mutex> lock(audio_queue_mutex_);
            if(codec_->ReferenceFaulted())++c3_pcm_.reference_faults;else ++c3_pcm_.media_faults;}
        // Pool/frame/network failures close this session without poisoning a
        // healthy reference engine. Real codec/DSP faults remain sticky.
        ResetDecoder();SetC3ServerReceiving(false);
        UpdateCaptureActive();
        ESP_LOGE(TAG,"C3 PCM media fault; session revoked reference_fault=%d",codec_->ReferenceFaulted());
    }
    return closed;
}
bool AudioService::FeedC3PcmSpan(const int16_t* samples,size_t count,uint32_t epoch) {
    bool fault=false,ready=false;
    {
        std::lock_guard<std::mutex> lock(audio_queue_mutex_);
        // Reset(0) adopts only a token checked under the same queue lock.
        if(service_stopped_.load() || !button_uplink_.Allows(epoch))return false;
        const bool reference_fault=codec_->ReferenceFaulted();
        fault=reference_fault || c3_pcm_.input.Feed(samples,count,epoch)!=family_robot::C3PcmResult::Ok;
        if(fault && !reference_fault)++c3_pcm_.input_faults;
        else ready=c3_pcm_.input.queued()!=0;
    }
    if(fault)RequestC3PcmFault(epoch);
    else if(ready && callbacks_.on_send_queue_available)callbacks_.on_send_queue_available();
    return !fault;
}
std::unique_ptr<AudioStreamPacket> AudioService::PopC3PcmPacket() {
    std::lock_guard<std::mutex> lock(audio_queue_mutex_);
    if(!button_uplink_.Capture()) {c3_pcm_.input.Reset();return nullptr;}
    if(!c3_pcm_.input.queued())return nullptr;
    auto packet=std::make_unique<AudioStreamPacket>();
    packet->payload.resize(family_robot::C3PcmInputPool::kBytes);
    if(c3_pcm_.input.Pop(packet->payload.data(),packet->payload.size(),&packet->uplink_epoch)!=family_robot::C3PcmResult::Ok)
        return nullptr;
    packet->sample_rate=16000;packet->frame_duration=60;
    return packet;
}
bool AudioService::PushC3PcmOutput(const uint8_t* bytes,size_t size,family_robot::C3PcmOutputMetadata metadata) {
    bool accepted=false;
    {
        std::lock_guard<std::mutex> lock(audio_queue_mutex_);
        if(!service_stopped_.load() && metadata.output_generation==codec_->PlaybackGeneration())
            accepted=c3_pcm_.output.Push(bytes,size,metadata)==family_robot::C3PcmResult::Ok;
        if(accepted) {playback_drained_notified_=false;audio_queue_cv_.notify_all();}
        else {
            const family_robot::PlaybackIdentity id{metadata.connection,metadata.playback};
            codec_->PlaybackControl(AudioCodec::PlaybackOp::Invalidate,id);
            codec_->PlaybackControl(AudioCodec::PlaybackOp::Release,id);
            ++c3_pcm_.output_faults;
        }
    }
    if(!accepted)RequestC3PcmFault(button_uplink_.Capture());
    return accepted;
}
void AudioService::CancelC3PcmCuesLocked() {
    const auto generation=c3_pcm_.cue_generation.load();
    if(generation==UINT32_MAX) {RequestC3PcmFault(button_uplink_.Capture());return;}
    c3_pcm_.cue_generation.store(generation+1);
    c3_pcm_.cue_head=c3_pcm_.cue_count=0;
    // Cues only reach DMA after the complete server tail drains. This focused
    // reset can therefore never discard an admitted server tail.
    if(c3_pcm_.cue_in_flight || c3_pcm_.cue_hardware)
        codec_->PlaybackControl(AudioCodec::PlaybackOp::Reset);
    c3_pcm_.cue_hardware=false;
}
void AudioService::SetC3ServerReceiving(bool receiving) {
    std::lock_guard<std::mutex> lock(audio_queue_mutex_);
    if(receiving)CancelC3PcmCuesLocked();
    c3_pcm_.receiving=receiving;
    audio_queue_cv_.notify_all();
}
void AudioService::ResetC3PcmOutputLocked() {
    CancelC3PcmCuesLocked();
    family_robot::C3PcmOutputPool::Lease lease;
    while(c3_pcm_.output.Acquire(lease)==family_robot::C3PcmResult::Ok) {
        codec_->PlaybackControl(AudioCodec::PlaybackOp::Release,{lease.metadata.connection,lease.metadata.playback});
        c3_pcm_.output.Release(lease);
    }
    c3_pcm_.output.Reset(); // in-flight output keeps its lease until worker returns
}
void AudioService::PlayC3PcmCue(const std::string_view& ogg) {
    uint8_t digest[32];
    if(!C3PcmSha256(reinterpret_cast<const uint8_t*>(ogg.data()),ogg.size(),digest)) {
        RequestC3PcmFault(button_uplink_.Capture());return;
    }
    bool fault=false;
    {
        std::lock_guard<std::mutex> lock(audio_queue_mutex_);
        family_robot::C3PcmCuePack::Descriptor descriptor;
        const auto generation=c3_pcm_.cue_generation.load();
        if(service_stopped_.load() || generation==UINT32_MAX ||
           !c3_pcm_.pack.Find(digest,descriptor) || c3_pcm_.cue_count==family_robot::C3PcmAudioState::kCues)fault=true;
        else {
            const auto tail=(c3_pcm_.cue_head+c3_pcm_.cue_count)%family_robot::C3PcmAudioState::kCues;
            c3_pcm_.cues[tail]={descriptor,0,generation,codec_->PlaybackGeneration()};++c3_pcm_.cue_count;
            playback_drained_notified_=false;audio_queue_cv_.notify_all();
        }
    }
    if(fault)RequestC3PcmFault(button_uplink_.Capture());
}
bool AudioService::C3PcmPlaybackDrainedLocked() const {
    return !c3_pcm_.output.occupied() && !c3_pcm_.cue_count && !c3_pcm_.cue_in_flight &&
        !output_in_flight_ && !codec_->PlaybackBusy();
}
void AudioService::C3PcmOutputTask() {
    for(;;) {
        family_robot::C3PcmOutputPool::Lease lease;
        family_robot::C3PcmAudioState::Cue cue;
        bool from_cue=false;
        const int16_t* samples=nullptr;
        {
            std::unique_lock<std::mutex> lock(audio_queue_mutex_);
            // DMA completion is ISR-owned; a bounded wait also notices hardware
            // drain without posting an allocating ISR queue/event for each DMA.
            audio_queue_cv_.wait_for(lock,std::chrono::milliseconds(10),[this]{
                return service_stopped_.load() || c3_pcm_.output.queued() ||
                    (!c3_pcm_.receiving && c3_pcm_.cue_count && (c3_pcm_.cue_hardware || !codec_->PlaybackBusy()));});
            if(service_stopped_.load())break;
            if(c3_pcm_.output.Acquire(lease)==family_robot::C3PcmResult::Ok)samples=lease.samples;
            else if(!c3_pcm_.receiving && c3_pcm_.cue_count && !c3_pcm_.output.occupied() && (c3_pcm_.cue_hardware || !codec_->PlaybackBusy())) {
                cue=c3_pcm_.cues[c3_pcm_.cue_head];
                if(cue.generation!=c3_pcm_.cue_generation.load()) {CancelC3PcmCuesLocked();continue;}
                if(c3_pcm_.pack.CopyFrame(cue.descriptor,cue.frame,c3_pcm_.cue_scratch,1440)!=family_robot::C3PcmCuePack::CopyResult::Ok) {
                    CancelC3PcmCuesLocked();RequestC3PcmFault(button_uplink_.Capture());continue;
                }
                auto& front=c3_pcm_.cues[c3_pcm_.cue_head];++front.frame;
                if(front.frame==front.descriptor.samples/1440) {c3_pcm_.cue_head=(c3_pcm_.cue_head+1)%family_robot::C3PcmAudioState::kCues;--c3_pcm_.cue_count;}
                from_cue=true;samples=c3_pcm_.cue_scratch;c3_pcm_.cue_in_flight=true;c3_pcm_.cue_hardware=true;
            }
            if(!samples)continue;
            output_in_flight_=true;++output_activity_;
        }
        if(!codec_->output_enabled()) {
            esp_timer_stop(audio_power_timer_);
            esp_timer_start_periodic(audio_power_timer_,AUDIO_POWER_CHECK_INTERVAL_MS*1000);
            codec_->EnableOutput(true);
        }
        bool cue_current=true;
        if(from_cue) {
            std::lock_guard<std::mutex> lock(audio_queue_mutex_);
            cue_current=!service_stopped_.load() && !c3_pcm_.receiving && cue.generation==c3_pcm_.cue_generation.load();
            if(cue_current && cue.frame==0) {
                // Enabling OUT advances the hardware generation. Ticket only
                // this new whole cue after power-up; an old tail is never reticketed.
                cue.output_generation=codec_->PlaybackGeneration();
                if(cue.descriptor.samples>1440) {
                    auto& front=c3_pcm_.cues[c3_pcm_.cue_head];
                    cue_current=c3_pcm_.cue_count && front.generation==cue.generation &&
                        front.descriptor.index==cue.descriptor.index && front.frame==1;
                    if(cue_current)front.output_generation=cue.output_generation;
                }
            }
        }
        const auto metadata=lease.metadata;
        if(cue_current)codec_->OutputTaggedSpan(samples,1440,from_cue?family_robot::PlaybackIdentity{}:
            family_robot::PlaybackIdentity{metadata.connection,metadata.playback},from_cue?0:metadata.first,
            from_cue?cue.output_generation:metadata.output_generation);
        bool notify=false;
        {
            std::lock_guard<std::mutex> lock(audio_queue_mutex_);
            if(from_cue)c3_pcm_.cue_in_flight=false;
            else {
                codec_->PlaybackControl(AudioCodec::PlaybackOp::Release,{metadata.connection,metadata.playback});
                if(c3_pcm_.output.Release(lease)!=family_robot::C3PcmResult::Ok)RequestC3PcmFault(button_uplink_.Capture());
            }
            output_in_flight_=false;
            last_output_time_=std::chrono::steady_clock::now();++debug_statistics_.playback_count;
            notify=MarkPlaybackDrainedLocked();audio_queue_cv_.notify_all();
        }
        if(notify && callbacks_.on_playback_drained)callbacks_.on_playback_drained();
    }
}
void AudioService::StartC3Pcm() {
    std::lock_guard<std::mutex> lifecycle(c3_service_lifecycle_);
    if(!service_stopped_.load())return;
    if(c3_lifecycle_failed_ || audio_input_task_handle_ || audio_output_task_handle_) {codec_->ReportReferenceFailure();return;}
    audio_input_task_handle_=audio_output_task_handle_=opus_codec_task_handle_=nullptr;
    constexpr EventBits_t exits=AS_C3_INPUT_EXIT|AS_C3_OUTPUT_EXIT;
    xEventGroupClearBits(event_group_,exits|AS_C3_TASKS_GO|AS_EVENT_AUDIO_TESTING_RUNNING|
        AS_EVENT_WAKE_WORD_RUNNING|AS_EVENT_AUDIO_PROCESSOR_RUNNING|AS_EVENT_AUDIO_INPUT_STOP_REQUEST);
    const auto input=xTaskCreate([](void* arg){
        auto* self=static_cast<AudioService*>(arg);
        xEventGroupWaitBits(self->event_group_,AS_C3_TASKS_GO,pdFALSE,pdTRUE,portMAX_DELAY);
        if(!self->service_stopped_.load())self->AudioInputTask();
        xEventGroupSetBits(self->event_group_,AS_C3_INPUT_EXIT);vTaskDelete(nullptr);
    },"audio_input",8192,this,8,&audio_input_task_handle_);
    const auto output=input==pdPASS?xTaskCreate([](void* arg){
        auto* self=static_cast<AudioService*>(arg);
        xEventGroupWaitBits(self->event_group_,AS_C3_TASKS_GO,pdFALSE,pdTRUE,portMAX_DELAY);
        if(!self->service_stopped_.load())self->C3PcmOutputTask();
        xEventGroupSetBits(self->event_group_,AS_C3_OUTPUT_EXIT);vTaskDelete(nullptr);
    },"audio_output",4096,this,4,&audio_output_task_handle_):pdFAIL;
    if(input!=pdPASS || output!=pdPASS) {
        c3_lifecycle_failed_=true;codec_->ReportReferenceFailure();
        if(input!=pdPASS)xEventGroupSetBits(event_group_,AS_C3_INPUT_EXIT);
        if(output!=pdPASS)xEventGroupSetBits(event_group_,AS_C3_OUTPUT_EXIT);
        xEventGroupSetBits(event_group_,AS_C3_TASKS_GO);
        const auto done=xEventGroupWaitBits(event_group_,exits,pdFALSE,pdTRUE,pdMS_TO_TICKS(2000));
        if((done&exits)==exits)audio_input_task_handle_=audio_output_task_handle_=nullptr;
        return;
    }
    service_stopped_.store(false);
    esp_timer_start_periodic(audio_power_timer_,1000000);
    xEventGroupSetBits(event_group_,AS_C3_TASKS_GO);
}
void AudioService::StopC3Pcm() {
    std::lock_guard<std::mutex> lifecycle(c3_service_lifecycle_);
    if(!codec_)return;
    SetExperimentUplink(false);
    esp_timer_stop(audio_power_timer_);service_stopped_.store(true);UpdateCaptureActive();
    xEventGroupSetBits(event_group_,AS_C3_TASKS_GO|AS_EVENT_AUDIO_TESTING_RUNNING|AS_EVENT_WAKE_WORD_RUNNING|
        AS_EVENT_AUDIO_PROCESSOR_RUNNING|AS_EVENT_AUDIO_INPUT_STOP_REQUEST);
    {
        std::lock_guard<std::mutex> lock(audio_queue_mutex_);
        ++playback_generation_;codec_->PlaybackControl(AudioCodec::PlaybackOp::Reset);
        ResetC3PcmOutputLocked();c3_pcm_.input.Reset();audio_queue_cv_.notify_all();
    }
    EventBits_t required=0;
    if(audio_input_task_handle_)required|=AS_C3_INPUT_EXIT;
    if(audio_output_task_handle_)required|=AS_C3_OUTPUT_EXIT;
    if(required) {
        // No queue, DSP, input-SRC or codec lock is held across the exit barrier.
        const auto done=xEventGroupWaitBits(event_group_,required,pdFALSE,pdTRUE,pdMS_TO_TICKS(2000));
        if((done&required)!=required) {c3_lifecycle_failed_=true;codec_->ReportReferenceFailure();return;}
    }
    audio_input_task_handle_=audio_output_task_handle_=nullptr;
}

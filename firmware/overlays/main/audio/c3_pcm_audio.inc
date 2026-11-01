// The trailer owns a separate mmap handle: generic Assets::UnApplyPartition
// cannot invalidate cue bytes while the output worker borrows the pack.
static bool C3PcmSha256(const uint8_t* bytes,size_t size,uint8_t* digest,family_robot::C3StartupHash* diagnostic=nullptr) {
    // IDF initializes PSA at startup; this public call is safely idempotent.
    if(diagnostic)diagnostic->state=family_robot::C3StartupStep::Entered;
    const auto initialized=psa_crypto_init();
    if(diagnostic)diagnostic->init_status=initialized;
    if(initialized!=PSA_SUCCESS) {
        if(diagnostic)diagnostic->state=family_robot::C3StartupStep::Failed;
        return false;
    }
    size_t length=0;
    const auto hashed=psa_hash_compute(PSA_ALG_SHA_256,bytes,size,digest,32,&length);
    const bool valid=hashed==PSA_SUCCESS && length==32;
    if(diagnostic) {
        diagnostic->hash_status=hashed;diagnostic->length=length;
        diagnostic->state=valid?family_robot::C3StartupStep::Succeeded:family_robot::C3StartupStep::Failed;
    }
    return valid;
}
static bool C3PcmVerifyHash(const uint8_t* bytes,size_t size,const uint8_t* expected,void* context) {
    auto* startup=static_cast<family_robot::C3StartupDiagnostics*>(context);
    auto* diagnostic=startup && startup->hash_count<2?&startup->hash[startup->hash_count++]:nullptr;
    uint8_t digest[32];if(!C3PcmSha256(bytes,size,digest,diagnostic))return false;
    const bool valid=!std::memcmp(digest,expected,32);
    if(diagnostic) {diagnostic->matched=valid;diagnostic->state=valid?family_robot::C3StartupStep::Succeeded:family_robot::C3StartupStep::Failed;}
    return valid;
}
bool AudioService::InitializeC3CuePack() {
    if(c3_pcm_.pack.count())return true;
    using Step=family_robot::C3StartupStep;
    const bool first=c3_startup_.pack_partition==Step::NotEntered;
    if(first)c3_startup_.pack_partition=Step::Entered;
    const auto* partition=esp_partition_find_first(ESP_PARTITION_TYPE_DATA,ESP_PARTITION_SUBTYPE_ANY,"assets");
    if(first && partition) {c3_startup_.partition_address=partition->address;c3_startup_.partition_size=partition->size;}
    constexpr size_t offset=0x50000,capacity=0x1b0000;
    if(!partition || partition->address!=0x600000 || partition->size<offset ||
       kFamilyC3PcmPackBytes>capacity || kFamilyC3PcmPackBytes>partition->size-offset ||
       uint64_t(partition->address)+offset+kFamilyC3PcmPackBytes>=0x800000) {
        if(first)c3_startup_.pack_partition=Step::Failed;
        return false;
    }
    if(first) {c3_startup_.pack_partition=Step::Succeeded;c3_startup_.map=Step::Entered;}
    const void* bytes=nullptr;
    const auto mapped=esp_partition_mmap(partition,offset,kFamilyC3PcmPackBytes,ESP_PARTITION_MMAP_DATA,&bytes,&c3_cue_mmap_);
    if(first) {c3_startup_.map_result=mapped;c3_startup_.map=mapped==ESP_OK?Step::Succeeded:Step::Failed;}
    if(mapped!=ESP_OK)return false;
    if(first)c3_startup_.pack_open=Step::Entered;
    if(!c3_pcm_.pack.Open(static_cast<const uint8_t*>(bytes),kFamilyC3PcmPackBytes,kFamilyC3PcmPackSha256,C3PcmVerifyHash,first?&c3_startup_:nullptr) ||
       c3_pcm_.pack.count()!=21) {
        if(first) {c3_startup_.pack_open=Step::Failed;c3_startup_.pack_count=c3_pcm_.pack.count();}
        esp_partition_munmap(c3_cue_mmap_);c3_cue_mmap_=0;return false;
    }
    if(first) {c3_startup_.pack_open=Step::Succeeded;c3_startup_.pack_count=c3_pcm_.pack.count();}
    return true;
}
void AudioService::RecordC3StartupHeap(family_robot::C3StartupStage stage) {
    // Initializer / lifecycle owner only. Never called from an ISR or portMUX.
    if(c3_startup_.heap_count==family_robot::C3StartupDiagnostics::kHeapStages)return;
    auto& entry=c3_startup_.heap[c3_startup_.heap_count];entry.stage=stage;entry.timestamp_us=esp_timer_get_time();
    entry.free=heap_caps_get_free_size(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    entry.largest=heap_caps_get_largest_free_block(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    entry.minimum=heap_caps_get_minimum_free_size(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    ++c3_startup_.heap_count;
}
void AudioService::PrintC3StartupDiagnostics() {
    family_robot::C3StartupDiagnostics d;
    {std::lock_guard<std::mutex> lock(c3_service_lifecycle_);d=c3_startup_;}
    const auto f=codec_->StartupDiagnostics();
    const auto current_us=esp_timer_get_time();
    const auto current_free=heap_caps_get_free_size(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    const auto current_largest=heap_caps_get_largest_free_block(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    const auto current_minimum=heap_caps_get_minimum_free_size(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
    ESP_LOGI(TAG,"C3 startup current at_us=%lld free/largest/min=%lu/%lu/%lu",
        (long long)current_us,(unsigned long)current_free,(unsigned long)current_largest,(unsigned long)current_minimum);
    ESP_LOGI(TAG,"C3 startup at_us=%lld fault=%lu us=%lld pair=%lu active/armed/open=%lu/%lu/%lu tx/rx_us=%lld/%lld samples=%lld/%lld prev_us=%lld",
        (long long)esp_timer_get_time(),(unsigned long)f.source,(long long)f.timestamp_us,(unsigned long)f.pair_fault,
        (unsigned long)f.capture_active,(unsigned long)f.callbacks_armed,(unsigned long)f.hardware_open,
        (long long)f.tx_us,(long long)f.rx_us,(long long)f.tx_samples,(long long)f.rx_samples,(long long)f.callback_previous_us);
    ESP_LOGI(TAG,"C3 startup hw mask=%lu channel=%lu get_tx/rx=%ld/%ld state=%lu",
        (unsigned long)f.hardware.mask,(unsigned long)f.hardware.channel,(long)f.hardware.tx_result,(long)f.hardware.rx_result,(unsigned long)f.hardware.state);
    const auto* a=f.hardware.actual;
    ESP_LOGI(TAG,"C3 startup hw actual dma/bclk/mclk/rate/width/slot/mode/mask/ws/pol/shift/align/endian/lsb=%lu/%lu/%lu/%lu/%lu/%lu/%lu/%lu/%lu/%lu/%lu/%lu/%lu/%lu",
        (unsigned long)a[0],(unsigned long)a[1],(unsigned long)a[2],(unsigned long)a[3],(unsigned long)a[4],(unsigned long)a[5],(unsigned long)a[6],
        (unsigned long)a[7],(unsigned long)a[8],(unsigned long)a[9],(unsigned long)a[10],(unsigned long)a[11],(unsigned long)a[12],(unsigned long)a[13]);
    ESP_LOGI(TAG,"C3 startup partition/map/open=%lu/%lu/%lu addr/size=%lu/%lu map_rc=%ld count=%lu hashes=%lu src/engine/aec=%lu/%lu/%lu src_rc=%ld tasks=%lu/%lu task_rc=%ld/%ld",
        (unsigned long)d.pack_partition,(unsigned long)d.map,(unsigned long)d.pack_open,(unsigned long)d.partition_address,(unsigned long)d.partition_size,
        (long)d.map_result,(unsigned long)d.pack_count,(unsigned long)d.hash_count,(unsigned long)d.input_src,(unsigned long)d.audio_engine,(unsigned long)d.aec_create,
        (long)d.input_src_result,(unsigned long)d.input_task,(unsigned long)d.output_task,(long)d.input_task_result,(long)d.output_task_result);
    for(unsigned i=0;i<d.hash_count && i<2;++i) {
        const auto& h=d.hash[i];ESP_LOGI(TAG,"C3 startup hash=%u state=%lu init/compute=%ld/%ld length/match=%lu/%lu",i,
            (unsigned long)h.state,(long)h.init_status,(long)h.hash_status,(unsigned long)h.length,(unsigned long)h.matched);
    }
    for(unsigned i=0;i<d.heap_count;++i) {
        const auto& h=d.heap[i];ESP_LOGI(TAG,"C3 startup heap stage=%lu us=%lld free/largest/min=%lu/%lu/%lu",
            (unsigned long)h.stage,(long long)h.timestamp_us,(unsigned long)h.free,(unsigned long)h.largest,(unsigned long)h.minimum);
    }
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
    using Step=family_robot::C3StartupStep;
    const bool first=c3_startup_.input_task==Step::NotEntered;
    if(first)RecordC3StartupHeap(family_robot::C3StartupStage::Start);
    audio_input_task_handle_=audio_output_task_handle_=opus_codec_task_handle_=nullptr;
    constexpr EventBits_t exits=AS_C3_INPUT_EXIT|AS_C3_OUTPUT_EXIT;
    xEventGroupClearBits(event_group_,exits|AS_C3_TASKS_GO|AS_EVENT_AUDIO_TESTING_RUNNING|
        AS_EVENT_WAKE_WORD_RUNNING|AS_EVENT_AUDIO_PROCESSOR_RUNNING|AS_EVENT_AUDIO_INPUT_STOP_REQUEST);
    if(first)c3_startup_.input_task=Step::Entered;
    const auto input=xTaskCreate([](void* arg){
        auto* self=static_cast<AudioService*>(arg);
        xEventGroupWaitBits(self->event_group_,AS_C3_TASKS_GO,pdFALSE,pdTRUE,portMAX_DELAY);
        if(!self->service_stopped_.load())self->AudioInputTask();
        xEventGroupSetBits(self->event_group_,AS_C3_INPUT_EXIT);vTaskDelete(nullptr);
    },"audio_input",8192,this,8,&audio_input_task_handle_);
    if(first) {
        c3_startup_.input_task_result=input;c3_startup_.input_task=input==pdPASS?Step::Succeeded:Step::Failed;
        RecordC3StartupHeap(family_robot::C3StartupStage::InputTaskComplete);
        if(input==pdPASS)c3_startup_.output_task=Step::Entered;
    }
    const auto output=input==pdPASS?xTaskCreate([](void* arg){
        auto* self=static_cast<AudioService*>(arg);
        xEventGroupWaitBits(self->event_group_,AS_C3_TASKS_GO,pdFALSE,pdTRUE,portMAX_DELAY);
        if(!self->service_stopped_.load())self->C3PcmOutputTask();
        xEventGroupSetBits(self->event_group_,AS_C3_OUTPUT_EXIT);vTaskDelete(nullptr);
    },"audio_output",4096,this,4,&audio_output_task_handle_):pdFAIL;
    if(first) {
        if(input==pdPASS) {c3_startup_.output_task_result=output;c3_startup_.output_task=output==pdPASS?Step::Succeeded:Step::Failed;}
        RecordC3StartupHeap(family_robot::C3StartupStage::OutputTaskComplete);
    }
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
    if(first)RecordC3StartupHeap(family_robot::C3StartupStage::StartComplete);
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

// Only the button-entry variant uses capture epochs in the lite audio engine.
// FeedCaptured runs on the single input task; queued Opus work keeps its epoch.
void LiteAudioEngine::FeedCaptured(std::vector<int16_t>&& data, uint32_t generation) {
#if CONFIG_XMINI_C3_VOICE_BARGE
    if(!codec_ || !c3_aec_ || !generation || !voice_processing_enabled_.load())return;
    if(frame_samples_<=0) {codec_->ReportReferenceFailure();return;}
    const auto frame_samples=static_cast<size_t>(frame_samples_);
    std::lock_guard<std::mutex> lock(output_mutex_);
    const auto capture=codec_->CaptureGeneration();
    auto current=[&] {
        return voice_processing_enabled_.load() && codec_->CaptureGeneration()==capture &&
            codec_->ReferenceValid() && !codec_->ReferenceFaulted();
    };
    if(!current())return;
    if(buffer_epoch_!=generation || c3_capture_generation_!=capture) {
        output_buffer_.clear();buffer_epoch_=generation;c3_capture_generation_=capture;
        codec_->NotifyAecReady(false,capture);
        if(!c3_aec_->Begin(generation,capture)) {codec_->ReportReferenceFailure();return;}
    }
    capture_epoch_.store(generation);
    const auto result=c3_aec_->Feed(data.data(),data.size()/2,generation,capture,current,
        [&](const int16_t* output,size_t count) {
            if(!current())return;
            codec_->NotifyAecReady(true,capture);
            while(count) {
                if(!current()) {output_buffer_.clear();return;}
                // AEC emits 512 samples; fill only the remaining Opus-frame
                // space so vector growth never doubles the working PCM store.
                output_buffer_.reserve(frame_samples);
                const auto take=std::min(count,frame_samples-output_buffer_.size());
                output_buffer_.insert(output_buffer_.end(),output,output+take);
                output+=take;count-=take;
                if(output_buffer_.size()==frame_samples) {
                    // The queue takes this allocation; no second frame copy.
                    output_callback_(std::move(output_buffer_));
                    output_buffer_.clear();
                }
            }
        });
    if(result!=family_robot::C3LiteAec::Result::Ok) {
        output_buffer_.clear();codec_->NotifyAecReady(false,capture);
        if(result==family_robot::C3LiteAec::Result::Fault)codec_->ReportReferenceFailure();
    }
#else
    capture_epoch_.store(generation);
    Feed(std::move(data));
#endif
}

void LiteAudioEngine::OutputRawAudio(const std::vector<int16_t>& data) {
#if CONFIG_XMINI_C3_VOICE_BARGE
    (void)data; // This variant can only publish successfully processed matched MR.
#else
    if (!output_callback_ || codec_ == nullptr) return;
    std::lock_guard<std::mutex> lock(output_mutex_);
    const auto epoch = capture_epoch_.load();
    if (buffer_epoch_ != epoch) {
        output_buffer_.clear();
        buffer_epoch_ = epoch;
    }
    if (!epoch || !voice_processing_enabled_.load()) return;
    const size_t channels = codec_->input_channels();
    if (channels <= 1) {
        output_buffer_.insert(output_buffer_.end(), data.begin(), data.end());
    } else {
        for (size_t i = 0; i < data.size(); i += channels) output_buffer_.push_back(data[i]);
    }
    while (output_buffer_.size() >= static_cast<size_t>(frame_samples_)) {
        if (output_buffer_.size() == static_cast<size_t>(frame_samples_)) {
            output_callback_(std::move(output_buffer_));
            output_buffer_.clear();
            output_buffer_.reserve(frame_samples_);
        } else {
            output_callback_(std::vector<int16_t>(output_buffer_.begin(),
                                                output_buffer_.begin() + frame_samples_));
            output_buffer_.erase(output_buffer_.begin(), output_buffer_.begin() + frame_samples_);
        }
    }
#endif
}

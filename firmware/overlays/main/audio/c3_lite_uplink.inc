// Only the button-entry variant uses capture epochs in the lite audio engine.
// FeedCaptured runs on the single input task; queued Opus work keeps its epoch.
void LiteAudioEngine::FeedCaptured(std::vector<int16_t>&& data, uint32_t generation) {
    capture_epoch_.store(generation);
    Feed(std::move(data));
}

void LiteAudioEngine::OutputRawAudio(const std::vector<int16_t>& data) {
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
}

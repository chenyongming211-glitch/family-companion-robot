import AVFoundation

func microphoneConverter(from source: AVAudioFormat) throws -> AVAudioConverter {
    guard source.sampleRate > 0, source.channelCount > 0,
          let mono = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: 16000, channels: 1, interleaved: true),
          let converter = AVAudioConverter(from: source, to: mono) else {
        throw ClientFailure("microphone_format")
    }
    // Voice Processing on this Mac exposes six discrete copies of its mic
    // signal. Layout-based remapping to mono otherwise selects silence.
    // Select the microphone's first signal channel explicitly, then resample.
    converter.channelMap = [0]
    return converter
}

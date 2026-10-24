import AVFoundation
import Foundation

@main struct MicrophoneSpec {
    static func main() throws {
        for (channels, microphoneSignal, otherSignals) in [(1, true, true), (2, true, true), (6, true, true), (6, true, false), (6, false, true)] {
            let layout = AVAudioChannelLayout(layoutTag: kAudioChannelLayoutTag_DiscreteInOrder | UInt32(channels))!
            let source = AVAudioFormat(standardFormatWithSampleRate: 48000, channelLayout: layout)
            let input = AVAudioPCMBuffer(pcmFormat: source, frameCapacity: 4800)!
            input.frameLength = 4800
            for channel in 0..<channels {
                for i in 0..<4800 {
                    let audible = channel == 0 ? microphoneSignal : otherSignals
                    input.floatChannelData![channel][i] = audible ? Float(sin(Double(i) * 2 * .pi * 440 / 48000) * 0.2) : 0
                }
            }
            let converter = try microphoneConverter(from: source)
            let output = AVAudioPCMBuffer(pcmFormat: converter.outputFormat, frameCapacity: 2000)!
            var supplied = false
            var error: NSError?
            let status = converter.convert(to: output, error: &error) { _, state in
                if supplied { state.pointee = .noDataNow; return nil }
                supplied = true; state.pointee = .haveData; return input
            }
            precondition(status != .error && error == nil && output.frameLength > 0, "microphone conversion failed")
            let samples = UnsafeBufferPointer(start: output.int16ChannelData![0], count: Int(output.frameLength))
            let peak = samples.map { abs(Int($0)) }.max() ?? 0
            if microphoneSignal {
                precondition(peak > 1000, "nonzero microphone became silent for \(channels) channels")
            } else {
                precondition(peak == 0, "other discrete channels leaked into microphone mono")
            }
        }
        print("PASS microphone: mono/stereo/6-channel input survives 48k to16k; first microphone channel stays isolated")
    }
}

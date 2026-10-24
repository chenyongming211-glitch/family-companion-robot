# AEC implementation research — not an implementation plan or acceptance

Primary main contains only stock build foundation and optional interrupted-stop metadata. Device firmware is still unchanged.

## Confirmed source evidence

- Locked upstream:49b4380; StopWatch config uses physical mono ES8311 at24kHz. Generic AudioCodec input/output virtual methods permit a board-specific adapter without changing hardware pins.
- Registry esp_codec_dev1.6.2 links to esp-adf commitaf7b72fb2b73d4f513d3cede01c13518ab216735. Exact30470-byte platform/audio_codec_data_i2s.c retained here with blob/SHA metadata. Do not substitute latest2.x APIs.
- Component lines625–633 rewrite logical physical fs.channel1 into channel2/maskLEFT; get_active_channel then yields1 and set_drv_fs creates MONO16-bit slots. DMA channel stride must follow the active reconfigured format, not constructor's initial STEREO config.
- Component read/write delegate to i2s_channel_read/write, no additional PCM transformation in those functions. They return codec success/error rather than sample counts; generic ES8311 Read currently returns requested count even after error. A new experiment must detect its own integrity failures rather than trusting this count.
- Component set_fs can reconfigure and disable/re-enable paired clocks. Capture/reference generations must reset on such changes. Callback registration must occur while corresponding I2S channel disabled.
- SDK6.1 common header documents ISR on_sent as completed DMA buffer (copy before auto_clear_after_cb); on_recv is completed ADC DMA buffer. Driver full duplex shares clocks but does not guarantee synchronous start. Never assert automatic TX/RX sample alignment.
- Upstream software-reference example BoxAudioCodecLite exists, but its shared non-atomic read/write positions are not suitable for blind reuse. It copies submitted PCM, which does not establish DMA or acoustic timing.
- AudioService resamples logical channels together24k→16k, which is useful for keeping an MR pair on the same resampling history. Do not independently round packet counts.
- ResetDecoder already increments playback_generation and clears decoder/playback queues under locks; decode tasks use generation. Already submitted I2S or one in-flight OutputData chunk is not physically erased; retain reference for this real tail.
- Application SetAecMode(kAecOff) schedules AEC disable and audio-channel close. Call application mutations via Schedule from a task, never directly from ISR. This is an existing fail-closed route back to default auto mode on next wake.

## Integration issues requiring an explicit implementation decision

- A timestamped bounded TX reference plus RX DMA sample position is needed to avoid treating esp_codec_dev_read completion time as microphone capture time. Using a FIFO from Write without timestamps is not adequate evidence.
- Candidate: copy TX and RX DMA samples into fixed internal-RAM rings; combine mic/reference at a common sample timeline, then use the existing joint resampler. ISR work must be bounded, no allocation/logging/application actions. Reference samples+metadata must stay within the approved32KB cap. An RX ring is a separate bounded resource. This remains a candidate, not implemented or selected.
- If bypassing i2s_channel_read and consuming RX from callback-owned samples, the driver's unused internal receive queue will overflow normally. Its overflow callback cannot be used as evidence of our own data loss; monitor the experiment's own ring/counters. Need confirm safe driver callback semantics in exact SDK source before implementing bypass.
- Clock start/reconfiguration, genuine DMA zeros, output starvation, expected silence, incomplete reference, overflow and disconnect must be distinguished. No invented zero reference during actual playback; integrity fault must terminate the realtime experiment.
- Fixed TX/RX offset and acoustic delay must be measured from private mic/reference captures. Callback timestamps are useful but are not speaker acoustic timestamps; do not claim field alignment from code or tests.
- Diagnostic collection must be limited to an explicit test window and must include sample positions/sequence loss. Existing AudioDebugger only sends raw input via compile-time UDP destination and has no matching AEC-output timeline; it is not an adequate three-stream capture by itself. Normal runs must not record audio/transcripts.
- Experimental variant must uniquely name its release, keep original variant/pins unchanged, and keep normal wake behavior. Enable realtime only after duplex/AEC measurement; no local-VAD abort decision in first stage.
- Optional gateway reason field already has wire-level regression coverage on main; deployment remains unperformed. Firmware must act only on interrupted reason and preserve normal stop draining and MCP controls.

Exact pinned SDK i2s_common.c retained (Git blobcc513df4d66a3a99452a80d4cc7d6eea70dc0079). GDMA RXcallback lines793–819 calls on_recv before internal queue-full eviction and queue insert, so callback can copy every completed RX DMA block independently of draining the normal read queue. GDMA TXcallback lines824–866 calls on_sent before auto_clear_after_cb; locked ES8311 sets before=false/after=true. Fixed-size copies from both callbacks are therefore a source-supported experiment candidate; callback timing/acoustic offset still require measurement, and no adapter is implemented.

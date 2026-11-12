# Waveshare music audition test client

This dedicated test path plays a bounded, licensed music excerpt over USB. Supported hardware is Waveshare ESP32-S3 2.16 only. It does not record or send microphone audio, alter device volume, add a music service, or provide a production music library. Firmware audition support is separately compiled and defaults off.

Prepare audio and run runtime tests on the cloud server. The Mac may run this USB test client later. Keep downloaded music, prepared WAV files, reports containing media paths and generated firmware outside Git. The WAV files below are licensed music, not user voice recordings.

## Source and shared gain

- “Carefree”, Kevin MacLeod (incompetech.com), ISRC USUAN1400037.
- [Official track](https://incompetech.com/music/royalty-free/index.html?isrc=USUAN1400037), [official MP3](https://incompetech.com/music/royalty-free/mp3-royaltyfree/Carefree.mp3), [official metadata](https://incompetech.com/music/royalty-free/pieces.json).
- [Creative Commons Attribution 4.0](https://creativecommons.org/licenses/by/4.0/).

Cloud preparation requires existing Python 3, FFmpeg and ffprobe, with no Python package dependency:

```sh
python3 tools/waveshare-music-audition/prepare_audio.py \
  --output /home/ubuntu/music-audition-20261112/assets-carefree \
  --start 30 --seconds 30
```

Use an empty output directory; a failed attempt remains available as evidence. The script downloads the official source, page, matching metadata and license and records their SHA256 values and final HTTPS URLs. It checks the title/ISRC and stereo audio format. It excerpts 30–60 seconds and averages the stereo channels once (`0.5*left + 0.5*right`) into one float master. One static gain targets −23 LUFS while keeping the shared master below −2 dBTP. The gained float master is then converted separately to 24kHz and 48kHz PCM16 mono WAV. No independent per-output normalization occurs. Each final output records exact samples, SHA256, measured integrated LUFS, true peak and integer sample peak. Integer clipping and final true peak at/above −0.1 dBTP reject the preparation.

The official source is a lossy MP3. Upsampling to 48kHz cannot recover missing high-frequency content or detail and does not make it a native 48kHz or high-resolution source. This A/B compares output profiles derived from the same decoded master.

`manifest.json` records the actual common gain, measurements and hashes. `ATTRIBUTION.txt` retains credit, license, source and all modifications; preserve it with any copy of the WAVs. Checksums identify the observed official download; they are not a publisher-provided signature. The target may be limited by peak headroom, and resampling can cause small loudness differences. Keep hardware volume unchanged (current trial: 80) and compare at the same location. These digital measurements do not establish equal acoustic loudness or improved sound quality.

## USB client

Run only after the audition-enabled candidate is installed and the device is idle. Install pyserial in the test client's environment if it is not already available. Copy only the two licensed output WAVs, manifest and attribution from the cloud, outside the repository; do not copy server credentials. Do not stream individual blocks through SSH, whose round-trip delay can exceed the 30/60ms audio block duration.

```sh
python3 tools/waveshare-music-audition/client.py \
  --port /dev/cu.usbmodemDEVICE \
  --wav /absolute/outside-repository/carefree-24000.wav --rate 24000
python3 tools/waveshare-music-audition/client.py \
  --port /dev/cu.usbmodemDEVICE \
  --wav /absolute/outside-repository/carefree-48000.wav --rate 48000
```

The client opens the port with DTR/RTS deasserted to avoid requesting board reset. The transport also accepts pyserial `socket://host:port` URLs when an already authorized USB bridge is needed. The client validates the complete WAV for truncation before starting, and retains at most one PCM block. It waits for a matching device event before sending the next block. Ctrl-C requests STOP at the next block boundary; the physical button can also cancel. Other console lines are ignored. Malformed events, mismatching sequence/rate/count, short writes and a 5-second acknowledgment timeout abort transmission with restoration unconfirmed. Device idle/session deadlines remain responsible for cleanup if the connection is lost.

Output contains actual accepted device events only. `ACK` is flow control and its sample count is the device's physical completion count, which may lag sent samples. `DONE` requires the full announced sample count and verified restoration to 24kHz. `STOPPED` may contain a partial physical count, but also requires verified 24kHz restoration. Transmitted byte counts are never playback evidence. `ERROR` never proves rollback succeeded.

## Wire and state contract

All integer fields are little-endian. Header is exactly 24 bytes:

| Offset | Field | Meaning |
| --- | --- | --- |
| 0 | 4 bytes | ASCII `FMA1` |
| 4 | uint32 | operation: START=1, DATA=2, END=3, STOP=4 |
| 8 | uint32 | sequence |
| 12 | uint32 | argument |
| 16 | uint32 | payload bytes, at most 2880 |
| 20 | uint32 | IEEE/zlib CRC32 over header bytes 0–19 followed by payload |

START has sequence 0, argument 24000 or 48000, and an 8-byte uint64 total sample count in `1..sample_rate*120`. DATA has the next sequence, argument 0 and nonempty even-length PCM16 mono payload, at most 1440 samples. END has the next sequence, argument 0, no payload and requires the exact announced received count. STOP has the currently expected sequence, argument 0, no payload and permits partial count. Replays, gaps, invalid counts, malformed frames, 5 seconds without an accepted frame, backwards time and a 120-second total session deadline fail closed. A new valid START is then required. A second START during an active session is rejected.

Responses are newline-terminated ASCII:

```text
FMA1 <sequence> <READY|ACK|DONE|STOPPED|ERROR> <actual_rate> <physically_completed_samples>
```

READY is emitted only after successful profile switching; DONE/STOPPED only after drain/cancel and successful verified 24kHz restoration. Explicit responses echo their request's sequence. Physical cancellation echoes the in-flight request's sequence or, when idle between requests, the next expected sequence.

The platform-free header `firmware/overlays/main/audio/music_audition_protocol.h` owns wire validation, CRC and protocol sample/sequence/deadline state. Use `Session::AcceptBytes(bytes, size, now_ms, &frame)` so parse failures invalidate the session too. `Frame::payload` borrows the caller's frame buffer until that buffer is reused. `Accept` does no hardware work. END/STOP immediately set protocol `active()` false; the application must continue owning exclusion until hardware is restored. `CheckTimeout(now_ms)` invalidates an expired active session. Hardware profile switching, queue capacity, physical I2S completion, cancellation and response emission remain with the firmware application/audio adapter.

## Cloud checks

```sh
python3 tools/waveshare-music-audition/test_audition.py
python3 -m unittest discover -s firmware/tests -p test_music_audition.py
```

The Python suite uses isolated device-transport and stereo-sine fixtures. It tests client behavior and runs an actual FFmpeg preparation pipeline. The dedicated host wrapper compiles and executes the shared C++ protocol with strict warnings; the existing supported host-test discovery selects it automatically. These checks establish protocol/preparation behavior, not installation, hardware sample-rate readback, real acoustic playback or subjective A/B results.

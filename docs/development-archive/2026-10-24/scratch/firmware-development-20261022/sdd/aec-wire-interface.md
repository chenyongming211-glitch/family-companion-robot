# Settled Task 1 diagnostic interface (v1)

USB Serial/JTAG PRIMARY console in unique `m5stack-stopwatch-software-aec` variant.
Commands: `aec_stats`, `aec_diag_start <RFC1918 dotted IPv4> <port 1..65535> <seconds 1..120>`, `aec_diag_stop`, `aec_realtime on`, `aec_realtime off`. Strict single spaces for start command; max line95 characters. No capture at boot/connection; volatile realtime off at boot. On requires paired-reference valid AND a successful AEC fetch in the current capture generation; main task rechecks generation/eligibility before changing mode. Fixed calibration offset is build config `CONFIG_STOPWATCH_AEC_OFFSET_SAMPLES` [-2400,2400], positive looks back in TX24k timeline; default0.

UDP datagrams <=1200 bytes. Exactly48-byte header, all integers little endian, followed by signed PCM16 LE interleaved by frame. Do not use native C struct layout.

|Offset|Bytes|Field|
|---|---:|---|
|0|4|ASCII `SWAC`|
|4|1|version1|
|5|1|stream:1 raw MR after joint24k→16k resampler;2 AFE mono output before cloud encoding|
|6|1|channels:2 for raw MR,1 for AFE|
|7|1|flags bit0=reference valid at production; bit1=first packet after stream reset/window/generation; other bits0|
|8|4|per-stream sequence, monotonically increasing per capture window, including dropped enqueue attempts|
|12|4|capture generation (changes hardware restart, capture disarm/rearm, first latched fault)|
|16|8|stream-local sample-frame position; reset to0 on that stream's DSP reset or generation change|
|24|8|esp_timer monotonic microseconds at producer call, not acoustic sample timestamp|
|32|4|sample rate16000|
|36|2|sample-frame count (per channel)|
|38|2|payload byte length, frames*channels*2|
|40|4|aggregate diagnostic queue/send drops as observed at header creation|
|44|4|capture window id (increments on stop/start, volatile at reboot)|

Raw MR stream channel0=mic, channel1=completed-TX reference. Flag bit0 clear means fallback padded second slot and/or invalid reference: never score it as AEC-valid. AFE output represents independent DSP processing time; raw/AFE positions have independent origins and resets. AFE reset can happen on uplink mode transition while raw resampler continues. Do not align by equal sample position or claim zero DSP delay. Use flags, sequences, positions, capture generation and producer timestamps to segment; estimate alignment/delay acoustically within complete valid segments. A reboot resets monotonic timer/window/sequence and must start a new collection session.

Record is preallocated bounded queue (12 packets), no network on audio/ISR/main. Packets split only at whole frames, max raw288 frames or mono576. Sequence/position advance even if queue insertion drops. A lost reset flag is accompanied by a sequence gap and/or position discontinuity; treat gaps conservatively. Stop/expiry/window switch prevents worker transmitting queued old-window packets. Start can be issued over USB then unplugged for battery capture. Collector belongs to Task2 and is not implemented here.

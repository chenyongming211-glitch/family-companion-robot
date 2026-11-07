# Private robot business-audio capture

Cloud-only, independent Python standard-library tool for the first business
utterance of an explicitly selected Waveshare ESP32-S3. It passively reads the
existing Ethernet/IPv4/TCP/WebSocket v1 Opus uplink. It does not modify or connect
to the production service, change firmware, or run identity/model inference.
The WAV is decoded normal business uplink **after device DSP/AEC and Opus**;
it is not a raw microphone ADC recording.

Requires Linux Python 3.10+, existing `tcpdump`, `libopus.so.0`, and permission
to capture eth0. No Python packages or runtime dependencies are installed.
Run tests and capture on the cloud server; Mac is for editing and Git only.
Never copy captured audio, credentials, pcap, or production configuration to Mac
or Git. No real audio is needed for the synthetic engineering tests.

## Tests

Copy only these source files into a new private cloud directory, then run:

```sh
umask 077
python3 -m unittest -v test_capture
```

Tests use constructed packets, native Opus synthetic roundtrips, temporary
private files, and real short-lived subprocess fixtures. They do not execute
tcpdump, access eth0, read production configuration, or use real human audio.
These are engineering checks, not acoustic or voiceprint acceptance.

## Operator-controlled capture

After review, with the participant informed and the target device available:

```sh
umask 077
python3 capture.py \
  --device "$TARGET_MAC" \
  --config /etc/family-robot/server.env \
  --output /opt/family-robot-validation/PRIVATE_RUN/evidence/NEW_SAMPLE
```

The absolute output directory must not already exist. Its immediate parent must
be owned by the operator with no group/world permissions. All path ancestors
are opened without following symlinks. The tool creates a 0700 directory and
0600 files with exclusive, no-follow creation. It refuses overwrites. Files are
written once and never appended to or replaced by this tool; ordinary 0600
owner permissions do not claim operating-system immutable flags.
Set `TARGET_MAC` only in the cloud operator's private shell to the authorized
target from the existing registry. Do not put real device IDs in committed
examples or test fixtures.

The config reader only interprets `ROBOT_DEVICE_TOKEN`, `ROBOT_ALLOWED_DEVICE`,
and `ROBOT_DEVICE_REGISTRY`; it does not source shell text. Config/registry must
be regular, bounded, no-follow files without group write/execute or any world
permissions. Group-read is accepted for the deployed root-owned 0640 config;
root can read the deployed service-owned 0600 registry unchanged. No credentials
or config values are printed or saved.

Wait for the flushed JSON event before manually waking the robot:

```json
{"event": "ready", "stage": "capture_readiness"}
```

It is emitted only after tcpdump confirms it is listening on eth0. Start a new
device TCP connection after readiness; an already-open connection is ignored.
The operator can restart/wake the device through its normal controls; the tool
does not do so. Say one new phrase of about 3–5 seconds after the wake prompt,
then wait for the first answer. Stop this invocation after the final event.

The final event contains fixed status/reason plus frame/sample counts. Exit0
means structural capture completeness with verified zero kernel drops; exit2
means incomplete. Neither status proves human speech content, usable voiced
duration, speaker identity, enrollment quality, or production acceptance.

## Integrity and bounds

The fixed child command is equivalent to:

```sh
tcpdump -n -i eth0 -s 0 -U -B 4096 -w - tcp port 18041
```

Pcap flows only through process memory. Nothing writes raw pcap, HTTP headers,
Bearer tokens, arbitrary event strings, transcripts, other-device audio, or
server audio to disk. The capture requires a newly observed SYN/SYN-ACK/ACK,
the specified MAC and exact configured token, HTTP101 with the request's correct
Sec-WebSocket-Accept, a valid client16kHz/server24kHz hello, and server scope.
Only masked client binary messages following scoped listen/start in realtime
or auto mode are saved. After client hello and server scope validation,
pre-listen wake packets advance the same decoder used for business input;
their decoded PCM is immediately discarded. Only discarded frame/sample
counts are retained for reporting. At most32000 wake samples are allowed
(33 complete960-sample packets); an additional packet fails before decode.
Wake Opus and PCM never enter the source/WAV files. A scoped pre-listen wake
abort is ignored. Scope-mismatched controls fail closed.

TCP capture order is authoritative. Identical retransmits and matching overlaps
are skipped; gaps, conflicts, truncation, resets, unsupported frames or limits
invalidate the sample. Out-of-order network traffic is conservatively rejected,
not reordered. ACKs beyond observed peer bytes also invalidate the sample,
including a missing final input frame acknowledged by the first server reply;
delayed ACKs and32-bit sequence wrap are supported. NIC checksum offload is not
treated as a failed checksum.
WebSocket segmentation, coalescing, fragmentation and ping/pong are supported
with fixed bounds. Close frames are incomplete; a clean client TCP FIN at a
message boundary is supported. No packet-loss concealment, FEC or zero filling
is requested from Opus. Every admitted packet must yield exactly960 samples.

The first server tts/start, scoped client listen/stop, or clean client FIN ends
the first input. Later uplink and model audio are excluded. An unfinished
opposite-direction message at a completion boundary invalidates the sample;
packets arriving after that boundary are never moved earlier to add samples.

Limits: 16MiB cumulative pcap, 32 newly observed flows, 1MiB history per TCP
direction, 16KiB HTTP headers, 64KiB text messages, 4096-byte binary messages,
125-byte controls, and15 seconds/250 packets of admitted Opus. `--audio-limit`
may lower that duration; `--wait-limit` may lower the180-second ceiling. First
audio must arrive within15 seconds of listen/start. Tcpdump startup must confirm
within5 seconds. The capture loop reserves4 seconds from the180-second maximum
for bounded TERM/wait/KILL cleanup. Timeout and malformed partial input produce
incomplete metadata. Kernel-drop statistics must be present and exactlyzero.
TERM and HUP directed at the recorder enter the same cleanup path and produce
a sanitized incomplete result; repeated TERM/HUP during cleanup are ignored.
Previous handlers are restored afterward. SIGKILL cannot run process cleanup.

## Private artifacts

`utterance.opusframes` starts with ASCII `ROBOTOPUS1\n`, followed by repeated
four-byte **big-endian unsigned packet lengths** and unchanged source Opus
packets. It is the source artifact, not Ogg Opus. No timestamps or device IDs
are embedded. `utterance.wav` is16kHz mono signed16-bit little-endian PCM and is
written only for a complete sample with verified zero drops. Incomplete runs
retain any admitted original packets and metadata, with no valid WAV output.

`metadata.json` contains fixed schema/status/reason, format, counts, byte bounds,
duration, sample-unit RMS/peak, libopus version, and SHA256 bindings of source,
WAV and validated server session ID. Only the session hash is saved, never the
session string. The configured target's model label is explicitly operator
declared, not inferred from MAC. `source_kind=private_human` records the intended
capture protocol; it is not a content classifier. `stage=capture_readiness`,
`human_acceptance=false` and `production_acceptance=false` remain fixed.

`decoder_policy=bounded_prelisten_warmup` records decode-before-discard ordering
for wake input, followed by business decode using the existing warm state.
`wake_discarded_frames` and `wake_discarded_samples` exclude business input.
`context_saved=false` means neither wake packets/PCM nor decoder state is saved.
Consequently, the saved business Opus packets alone cannot reproduce the warm
WAV: `source_only_redecode_is_server_pcm=false` explicitly prevents labeling a
fresh decoder's business-only output as the backend reference. Source Opus and
WAV retain separate hashes. Matching valid packet call order does not establish
a measured bitwise match with recorded server PCM; that PCM is not captured.
Native synthetic tests compare the full wake-plus-business sequence with an
independent fresh decoder that replays both, without assuming which live
frames a business-only fresh decoder would change. Older captures made with
the initial skip-before-decode policy retain their original interpretation;
they must not be relabeled as warm-state captures or used for enrollment.

Fixed failure reasons and terminal JSON summaries prevent raw exception text
from exposing protocol/config contents. Failure artifacts are unsuitable for
enrollment. Keep all artifacts cloud-private and assess human speech and the
separately preregistered speaker protocol independently.

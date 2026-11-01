"""Explicit, evaluation-only PCM adaptation for an untracked cloud archive.

Never imported by a service or normal build. Requires an explicit marker and
rejects Git checkouts. All replacements are validated before any file is written.
"""
from pathlib import Path
import sys

root = Path(sys.argv[1]).resolve()
repo = Path(__file__).resolve().parents[2]
if root == repo or (root / '.git').exists() or not (root / '.c3-pcm-assessment-only').is_file():
    raise SystemExit('requires a marked, isolated source archive outside a Git checkout')
pending = {}


def replace(name, old, new):
    text = pending.get(name, (root / name).read_text())
    if text.count(old) != 1:
        raise SystemExit(f'{name}: expected exactly one unchanged source anchor')
    pending[name] = text.replace(old, new, 1)


replace('internal/protocol/protocol.go',
'''func ParseHello(data []byte, version int) (Hello, error) {''',
'''func ParseHello(data []byte, version int) (Hello, error) {
\treturn parseHello(data, version, false)
}
// ParseHelloWithPCMTrial is enabled only by the isolated Realtime entry.
func ParseHelloWithPCMTrial(data []byte, version int) (Hello, error) {
\treturn parseHello(data, version, true)
}
func parseHello(data []byte, version int, allowPCM bool) (Hello, error) {''')
replace('internal/protocol/protocol.go',
'''\tif a.Format != "opus" || a.SampleRate != 16000 || a.Channels != 1 || (a.FrameDuration != 20 && a.FrameDuration != 40 && a.FrameDuration != 60) {''',
'''\tvalidFormat := a.Format == "opus"
\tif allowPCM && a.Format == "pcm_s16le" {
\t\tvalidFormat = h.Features["pcm_s16le_v1"] && h.Version == 1 && a.FrameDuration == 60
\t}
\tif !validFormat || a.SampleRate != 16000 || a.Channels != 1 || (a.FrameDuration != 20 && a.FrameDuration != 40 && a.FrameDuration != 60) {''')

replace('internal/realtimelive/xiaozhi_entry.go',
'''protocol.ParseHello(b, int(version[0]-'0'))''',
'''protocol.ParseHelloWithPCMTrial(b, int(version[0]-'0'))''')
replace('internal/realtimelive/xiaozhi_entry.go',
'''\t\tsession.wire.(*xiaozhiWire).mcp = hello.Features["mcp"]''',
'''\t\tsession.wire.(*xiaozhiWire).mcp = hello.Features["mcp"]
\t\tsession.wire.(*xiaozhiWire).pcm = hello.AudioParams.Format == "pcm_s16le"''')

replace('internal/realtimelive/xiaozhi_wire.go',
'''\tmcp              bool // Negotiated once from the authenticated hello.''',
'''\tmcp              bool // Negotiated once from the authenticated hello.
\tpcm              bool // Evaluation-only negotiated native PCM transport.''')
replace('internal/realtimelive/xiaozhi_wire.go',
'''\tpcm, err := codec.Decode(packet)
\tif err != nil {
\t\treturn nil, err
\t}''',
'''\tvar pcm []byte
\tif w.pcm {
\t\tif len(packet) != w.frameMS * 32 {
\t\t\treturn nil, errors.New("pcm_input_frame_size")
\t\t}
\t\tpcm = append([]byte(nil), packet...)
\t} else {
\t\tpcm, err = codec.Decode(packet)
\t\tif err != nil { return nil, err }
\t}''')
replace('internal/realtimelive/xiaozhi_wire.go',
'''\t\treturn w.ws.WriteJSON(protocol.Hello{Type: "hello", Version: w.version, Transport: "websocket", SessionID: w.scope, AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 24000, Channels: 1, FrameDuration: 60}, Features: features})''',
'''\t\tformat := "opus"
\t\tif w.pcm { format = "pcm_s16le"; features["pcm_s16le_v1"] = true }
\t\treturn w.ws.WriteJSON(protocol.Hello{Type: "hello", Version: w.version, Transport: "websocket", SessionID: w.scope, AudioParams: protocol.AudioParams{Format: format, SampleRate: 24000, Channels: 1, FrameDuration: 60}, Features: features})''')
replace('internal/realtimelive/xiaozhi_wire.go',
'''func (w *xiaozhiWire) sendAudio(id int64, packet []byte) error {
\tblock := w.blocks[id]''',
'''func (w *xiaozhiWire) sendAudio(id int64, packet []byte) error {
\tif w.pcm && len(packet) != 2880 { return errors.New("pcm_output_frame_size") }
\tblock := w.blocks[id]''')

replace('internal/realtimelive/session.go',
'''func (s *Session) binary(rt *liveTurn, packet []byte) (bool, error) {''',
'''func (s *Session) pcmTransport() bool {
\tw, ok := s.wire.(*xiaozhiWire)
\treturn ok && w.pcm
}
func (s *Session) binary(rt *liveTurn, packet []byte) (bool, error) {''')
replace('internal/realtimelive/session.go',
'''\tif codec != nil {
\t\tdecoded, e := codec.Decode(packet)
\t\tif e != nil {
\t\t\treturn true, e
\t\t}
\t\tconverted, e := resampler.Process(decoded)''',
'''\tif codec != nil || (s.pcmTransport() && resampler != nil) {
\t\tdecoded := packet
\t\tif codec != nil {
\t\t\tvar e error
\t\t\tdecoded, e = codec.Decode(packet)
\t\t\tif e != nil { return true, e }
\t\t}
\t\tconverted, e := resampler.Process(decoded)''')
replace('internal/realtimelive/session.go',
'''\tcodec, e := realtimeaudio.New(16000, s.inputFrame)
\tif e != nil {
\t\treturn e
\t}
\tdefer codec.Close()''',
'''\tvar codec *realtimeaudio.Codec
\tvar e error
\tif !s.pcmTransport() {
\t\tcodec, e = realtimeaudio.New(16000, s.inputFrame)
\t\tif e != nil { return e }
\t}
\tdefer codec.Close()''')
replace('internal/realtimelive/session.go',
'''\tcodec, e := realtimeaudio.New(24000, 1440)
\tif e != nil {
\t\ts.turnError(rt)
\t\treturn
\t}
\tdefer codec.Close()''',
'''\tvar codec *realtimeaudio.Codec
\tvar e error
\tif !s.pcmTransport() {
\t\tcodec, e = realtimeaudio.New(24000, 1440)
\t\tif e != nil { s.turnError(rt); return }
\t}
\tdefer codec.Close()
\tencodeFrame := func(pcm []byte) ([]byte, error) {
\t\tif codec == nil { return append([]byte(nil), pcm...), nil }
\t\treturn codec.Encode(pcm)
\t}''')
replace('internal/realtimelive/session.go', 'codec.Encode(pending[:2880])', 'encodeFrame(pending[:2880])')
replace('internal/realtimelive/session.go',
'''\t\t\ttarget := (total + 312 + 1439) / 1440''',
'''\t\t\ttail := 312
\t\t\tif codec == nil { tail = 0 }
\t\t\ttarget := (total + tail + 1439) / 1440''')
replace('internal/realtimelive/session.go', 'codec.Encode(chunk)', 'encodeFrame(chunk)')

for name, content in pending.items():
    (root / name).write_text(content)
print('Applied isolated PCM trial to:', ', '.join(sorted(pending)))

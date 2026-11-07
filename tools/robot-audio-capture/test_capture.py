"""Cloud-only synthetic engineering checks; no network capture or real audio."""
import base64
import ctypes
import hashlib
import io
import json
import math
import os
from pathlib import Path
import select
import signal
import stat
import struct
import subprocess
import sys
import tempfile
import time
import unittest

try:
    import capture as c
except ModuleNotFoundError:
    c = None

MAC = "02:00:00:00:00:01"
TOKEN = "t" * 64
KEY = base64.b64encode(bytes(range(16))).decode()
ACCEPT = base64.b64encode(hashlib.sha1((KEY + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()


def ws(value, client=True, opcode=None, final=True):
    if opcode is None:
        opcode = 1 if isinstance(value, dict) else 2
    payload = json.dumps(value).encode() if isinstance(value, dict) else value
    n = len(payload)
    h = bytes([(128 if final else 0) | opcode])
    h += bytes([(128 if client else 0) | (n if n < 126 else 126)])
    if n >= 126:
        h += struct.pack("!H", n)
    if client:
        mask = b"test"
        return h + mask + bytes(v ^ mask[i % 4] for i, v in enumerate(payload))
    return h + payload


def hello(rate=16000):
    return {"type": "hello", "version": 1, "transport": "websocket", "audio_params": {"format": "opus", "sample_rate": rate, "channels": 1, "frame_duration": 60}}


def request(mac=MAC, token=TOKEN):
    return ("GET /xiaozhi/v1/ HTTP/1.1\r\nHost: server:18041\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + KEY + "\r\nProtocol-Version: 1\r\nDevice-Id: " + mac + "\r\nAuthorization: Bearer " + token + "\r\n\r\n").encode()


def response(accept=ACCEPT):
    return ("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n").encode()


def packet(client=True, seq=101, ack=501, flags=0x18, data=b""):
    src, dst = (b"\x01\x02\x03\x04", b"\x05\x06\x07\x08") if client else (b"\x05\x06\x07\x08", b"\x01\x02\x03\x04")
    ports = (34567, 18041) if client else (18041, 34567)
    tcp = struct.pack("!HHIIBBHHH", *ports, seq, ack, 5 << 4, flags, 65535, 0, 0) + data
    ip = struct.pack("!BBHHHBBH4s4s", 0x45, 0, 20 + len(tcp), 1, 0x4000, 64, 6, 0, src, dst)
    return b"\x00" * 12 + b"\x08\x00" + ip + tcp


def pcap(packets):
    header = struct.pack("<IHHIIII", 0xa1b2c3d4, 2, 4, 0, 0, 262144, 1)
    return header + b"".join(struct.pack("<IIII", i, 0, len(p), len(p)) + p for i, p in enumerate(packets))


def complete_pcap():
    packets = [packet(seq=100, ack=0, flags=2), packet(False, seq=500, ack=101, flags=0x12), packet(seq=101, ack=501, flags=0x10)]
    seqs = {True: 101, False: 501}
    server_hello = hello(24000); server_hello["session_id"] = "private-session"
    messages = [(True, request()), (False, response()), (True, ws(hello())), (False, ws(server_hello, False)), (True, ws({"type": "listen", "state": "start", "mode": "auto", "session_id": "private-session"}) + ws(b"business")), (False, ws({"type": "tts", "state": "start", "session_id": "private-session"}, False))]
    for client, data in messages:
        packets.append(packet(client, seqs[client], seqs[not client], data=data))
        seqs[client] += len(data)
    return pcap(packets)


class Base(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(c, "passive capture implementation is required")

    def bad(self, reason, fn):
        with self.assertRaises(c.CaptureError) as e:
            fn()
        self.assertEqual(e.exception.reason, reason)
        self.assertEqual(str(e.exception), reason)


class WireTests(Base):
    def test_tcp_split_duplicate_and_overlap(self):
        r = c.TCPStream(100)
        self.assertEqual(r.feed(100, b"ab"), b"ab")
        self.assertEqual(r.feed(100, b"ab"), b"")
        self.assertEqual(r.feed(101, b"bc"), b"c")
        self.bad("tcp_conflict", lambda: r.feed(101, b"X"))

    def test_tcp_gap_and_buffer_bound(self):
        r = c.TCPStream(100, limit=3)
        self.bad("tcp_gap", lambda: r.feed(101, b"a"))
        r.feed(100, b"abc")
        self.bad("tcp_limit", lambda: r.feed(103, b"d"))

    def test_tcp_wrap(self):
        r = c.TCPStream(0xffffffff)
        self.assertEqual(r.feed(0xffffffff, b"ab"), b"ab")
        self.assertEqual(r.feed(1, b"c"), b"c")

    def test_pcap_incremental_ethernet_tcp(self):
        p = c.PcapStream()
        out = []
        for b in pcap([packet(data=b"abc")]):
            out.extend(p.feed(bytes([b])))
        p.finish()
        self.assertEqual(len(out), 1)
        self.assertEqual(out[0].payload, b"abc")
        self.assertTrue(out[0].client)

    def test_pcap_truncated_record_and_unsupported(self):
        p = c.PcapStream()
        list(p.feed(pcap([packet()])[:-1]))
        self.bad("pcap_truncated", p.finish)
        self.bad("pcap_header", lambda: list(c.PcapStream().feed(b"x" * 24)))

    def test_pcap_capture_truncation_and_cap(self):
        raw = bytearray(pcap([packet()]))
        struct.pack_into("<I", raw, 36, len(packet()) + 1)
        self.bad("pcap_truncated", lambda: list(c.PcapStream().feed(raw)))
        self.bad("pcap_limit", lambda: list(c.PcapStream(limit=30).feed(pcap([packet()]))))

    def test_ipv4_fragments_and_bad_tcp_header(self):
        raw = bytearray(packet())
        raw[20:22] = b"\x20\x00"
        self.bad("ip_fragment", lambda: list(c.PcapStream().feed(pcap([raw]))))
        raw = bytearray(packet())
        raw[46] = 0x40
        self.bad("tcp_header", lambda: list(c.PcapStream().feed(pcap([raw]))))

    def test_ws_split_coalesce_fragment_and_ping(self):
        w = c.WebSocket(True)
        raw = ws(b"ab", opcode=2, final=False) + ws(b"hi", opcode=9) + ws(b"cd", opcode=0)
        out = []
        for b in raw:
            out.extend(w.feed(bytes([b])))
        self.assertEqual(out, [(9, b"hi"), (2, b"abcd")])
        self.assertTrue(w.idle())

    def test_ws_mask_control_and_fragment_errors(self):
        self.bad("ws_mask", lambda: list(c.WebSocket(True).feed(ws(b"x", client=False))))
        self.bad("ws_control", lambda: list(c.WebSocket(True).feed(ws(b"x", opcode=9, final=False))))
        self.bad("ws_fragment", lambda: list(c.WebSocket(True).feed(ws(b"x", opcode=0))))
        self.bad("ws_size", lambda: list(c.WebSocket(True).feed(ws(b"x" * 4097))))
        self.bad("ws_header", lambda: list(c.WebSocket(False).feed(b"\xc2\x00")))


class SessionTests(Base):
    def session(self, decoder=lambda p: b"\x01\x00" * 960):
        return c.Session(MAC, TOKEN, decoder, audio_limit=15)

    def start(self, s):
        s.feed(True, request())
        s.feed(False, response())
        s.feed(True, ws(hello()))
        h = hello(24000)
        h["session_id"] = "private-session"
        s.feed(False, ws(h, False))
        s.feed(True, ws({"type": "listen", "state": "start", "mode": "realtime", "session_id": "private-session"}))

    def test_upgrade_and_wake_audio_exclusion(self):
        s = self.session()
        s.feed(True, request())
        s.feed(False, response())
        s.feed(True, ws(hello()) + ws(b"wake"))
        h = hello(24000); h["session_id"] = "private-session"
        s.feed(False, ws(h, False))
        s.feed(True, ws({"type": "listen", "state": "start", "mode": "auto", "session_id": "private-session"}) + ws(b"real"))
        s.feed(False, ws({"type": "tts", "state": "start", "session_id": "private-session"}, False) + ws(b"model", False))
        self.assertEqual(s.frames, [b"real"])
        self.assertEqual(s.reason, "tts_start")
        self.assertTrue(s.complete)

    def test_wrong_target_and_token_are_rejected(self):
        for req in (request("11:22:33:44:55:66"), request(token="x" * 64)):
            s = self.session(); s.feed(True, req)
            self.assertTrue(s.rejected)
            self.assertFalse(s.authenticated)
            self.assertEqual(s.frames, [])

    def test_upgrade_accept_binds_request(self):
        s = self.session(); s.feed(True, request())
        self.bad("http_upgrade", lambda: s.feed(False, response("wrong")))

    def test_duplicate_http_headers_are_ambiguous(self):
        s = self.session()
        req = request().replace(b"Host: server:18041", b"Host: one\r\nHost: two")
        self.bad("http_header", lambda: s.feed(True, req))

    def test_only_hello_first_and_strict_client_format(self):
        for h in ({"type": "listen"}, hello(24000)):
            s = self.session(); s.feed(True, request()); s.feed(False, response())
            self.bad("hello_format", lambda: s.feed(True, ws(h)))

    def test_server_and_client_scope(self):
        for client, event in [(False, {"type": "tts", "state": "start", "session_id": "wrong"}), (True, {"type": "listen", "state": "stop", "session_id": "wrong"})]:
            s = self.session(); self.start(s); s.feed(True, ws(b"a"))
            self.bad("session_scope", lambda: s.feed(client, ws(event, client)))

    def test_first_reply_boundary_does_not_reorder_upstream(self):
        s = self.session(); self.start(s); s.feed(True, ws(b"first"))
        s.feed(False, ws({"type": "tts", "state": "start", "session_id": "private-session"}, False))
        s.feed(True, ws(b"later"))
        self.assertEqual(s.frames, [b"first"])

    def test_wake_abort_before_listen_is_ignored_with_scope_check(self):
        s = self.session()
        s.feed(True, request()); s.feed(False, response()); s.feed(True, ws(hello()))
        h = hello(24000); h["session_id"] = "private-session"; s.feed(False, ws(h, False))
        s.feed(True, ws({"type": "abort", "reason": "wake_word_detected", "session_id": "private-session"}))
        s.feed(True, ws({"type": "listen", "state": "start", "mode": "auto", "session_id": "private-session"}) + ws(b"business"))
        s.feed(False, ws({"type": "tts", "state": "start", "session_id": "private-session"}, False))
        self.assertTrue(s.complete)
        self.assertEqual(s.frames, [b"business"])

    def test_partial_upstream_at_reply_is_incomplete(self):
        s = self.session(); self.start(s); s.feed(True, ws(b"a") + ws(b"late")[:3])
        self.bad("boundary_partial", lambda: s.feed(False, ws({"type": "tts", "state": "start", "session_id": "private-session"}, False)))

    def test_client_stop_clean_fin_and_no_audio(self):
        for boundary in ("stop", "fin"):
            s = self.session(); self.start(s); s.feed(True, ws(b"a"))
            if boundary == "stop":
                s.feed(True, ws({"type": "listen", "state": "stop", "session_id": "private-session"}))
            else:
                s.fin(True)
            self.assertTrue(s.complete)
        s = self.session(); self.start(s)
        self.bad("empty_audio", lambda: s.fin(True))

    def test_duration_cap_and_decode_length(self):
        s = self.session(); self.start(s)
        for _ in range(250): s.feed(True, ws(b"a"))
        self.bad("audio_limit", lambda: s.feed(True, ws(b"b")))
        s = self.session(lambda p: b"x"); self.start(s)
        self.bad("opus_samples", lambda: s.feed(True, ws(b"a")))

    def test_new_handshake_required_and_gap_invalidates_selected(self):
        e = c.Engine(MAC, TOKEN, lambda p: b"\x01\x00" * 960)
        def send(raw):
            for p in c.PcapStream().feed(pcap([raw])): e.feed(p)
        send(packet(data=request()))
        self.assertIsNone(e.selected)
        send(packet(seq=100, ack=0, flags=2))
        send(packet(False, seq=500, ack=101, flags=0x12))
        send(packet(seq=101, ack=501, flags=0x10, data=request()))
        self.assertIsNotNone(e.selected)
        self.bad("tcp_gap", lambda: send(packet(seq=9999, data=b"x")))

    def test_server_ack_of_missing_final_input_invalidates_boundary(self):
        packets = list(c.PcapStream().feed(complete_pcap()))
        engine = c.Engine(MAC, TOKEN, lambda p: b"\x01\x00" * 960)
        for p in packets[:-1]: engine.feed(p)
        self.assertEqual(engine.session.frames, [b"business"])
        boundary = packets[-1]
        boundary.ack += len(ws(b"uncaptured-final-frame"))
        self.bad("tcp_gap", lambda: engine.feed(boundary))
        self.assertFalse(engine.session.complete)
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "incomplete"
            result = c.PrivateOutput(str(path)).finish(engine.session, "tcp_gap", True, 0)
            self.assertEqual(result["status"], "incomplete")
            self.assertFalse((path / "utterance.wav").exists())

    def test_delayed_ack_retransmit_and_wrap_are_safe(self):
        # Header-only ACKs may lag; numeric zero is valid after seq wrap.
        key = (b"client", 1, b"server", 18041)
        engine = c.Engine(MAC, TOKEN, lambda p: b"\x01\x00" * 960)
        first = c.Packet(key, True, 0xfffffff8, 0, 2, b"")
        engine.feed(first)
        engine.feed(c.Packet(key, False, 500, 0xfffffff9, 0x12, b""))
        engine.feed(c.Packet(key, True, 0xfffffff9, 501, 0x10, b""))
        req = request()
        engine.feed(c.Packet(key, True, 0xfffffff9, 501, 0x18, req))
        next_client = (0xfffffff9 + len(req)) & 0xffffffff
        engine.feed(c.Packet(key, False, 501, 0, 0x10, b""))
        engine.feed(c.Packet(key, False, 501, next_client, 0x18, response()))
        engine.feed(c.Packet(key, True, 0xfffffff9, 501, 0x18, req))
        self.assertTrue(engine.session.upgraded)
        self.assertFalse(engine.session.complete)
        self.bad("tcp_gap", lambda: engine.feed(c.Packet(key, False, 501 + len(response()), (next_client + 1) & 0xffffffff, 0x10, b"")))


class NativeAndStorageTests(Base):
    def test_real_opus_synthetic_roundtrip_960(self):
        decoder = c.OpusDecoder()
        lib = decoder.lib
        lib.opus_encoder_create.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_int, ctypes.POINTER(ctypes.c_int)]
        lib.opus_encoder_create.restype = ctypes.c_void_p
        lib.opus_encode.argtypes = [ctypes.c_void_p, ctypes.POINTER(ctypes.c_int16), ctypes.c_int, ctypes.c_void_p, ctypes.c_int]
        lib.opus_encoder_destroy.argtypes = [ctypes.c_void_p]
        err = ctypes.c_int()
        enc = lib.opus_encoder_create(16000, 1, 2049, ctypes.byref(err))
        self.assertEqual(err.value, 0)
        try:
            samples = (ctypes.c_int16 * 960)(*[int(5000 * math.sin(2 * math.pi * 440 * i / 16000)) for i in range(960)])
            out = ctypes.create_string_buffer(4096)
            n = lib.opus_encode(enc, samples, 960, out, 4096)
            self.assertGreater(n, 0)
            pcm = decoder.decode(out.raw[:n])
            self.assertEqual(len(pcm), 1920)
            self.assertGreater(max(struct.unpack("<960h", pcm)), 100)
            self.bad("opus_packet", lambda: decoder.decode(b""))
        finally:
            lib.opus_encoder_destroy(enc)
            decoder.close()

    def test_drop_stats_must_be_verified_zero(self):
        self.assertEqual(c.drop_status(b"1 packets captured\n2 packets received by filter\n0 packets dropped by kernel\n"), (True, 0))
        self.assertEqual(c.drop_status(b"1 packets dropped by kernel\n"), (True, 1))
        self.assertEqual(c.drop_status(b""), (False, None))

    def test_private_exclusive_output_and_bound_report(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "sample"
            output = c.PrivateOutput(str(path))
            s = c.Session(MAC, TOKEN, lambda p: b"\x01\x00" * 960)
            s.frames = [b"source"]; s.pcm = bytearray(b"\x01\x00" * 960); s.complete = True; s.reason = "tts_start"
            report = output.finish(s, None, True, 0)
            self.assertEqual(report["status"], "complete")
            self.assertFalse(report["production_acceptance"])
            self.assertFalse(report["human_acceptance"])
            self.assertEqual(report["source_device_basis"], "user_declared_and_configured_target")
            self.assertIsNone(report["session_sha256"])
            for f in path.iterdir(): self.assertEqual(stat.S_IMODE(f.stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o700)
            raw = (path / "utterance.opusframes").read_bytes()
            self.assertEqual(hashlib.sha256(raw).hexdigest(), report["source_sha256"])
            wav = (path / "utterance.wav").read_bytes()
            self.assertEqual(wav[:4], b"RIFF")
            self.assertEqual(hashlib.sha256(wav).hexdigest(), report["wav_sha256"])
            for private in (MAC, TOKEN, "private-session"): self.assertNotIn(private, json.dumps(report))
            self.bad("output_unsafe", lambda: c.PrivateOutput(str(path)))

    def test_session_hash_binds_validated_scope_without_exporting_scope(self):
        with tempfile.TemporaryDirectory() as tmp:
            s = c.Session(MAC, TOKEN, lambda p: b"")
            s.scope = "private-session"
            result = c.PrivateOutput(str(Path(tmp) / "sample")).finish(s, "timeout", True, 0)
            self.assertEqual(result["session_sha256"], hashlib.sha256(b"private-session").hexdigest())
            self.assertNotIn("private-session", json.dumps(result))

    def test_failure_has_no_valid_wav_and_source_retained(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "failed"
            s = c.Session(MAC, TOKEN, lambda p: b""); s.frames = [b"partial"]
            report = c.PrivateOutput(str(path)).finish(s, "tcp_gap", True, 0)
            self.assertEqual(report["status"], "incomplete")
            self.assertFalse((path / "utterance.wav").exists())
            self.assertTrue((path / "utterance.opusframes").exists())

    def test_symlink_and_public_parent_are_refused(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); (root / "link").symlink_to(root, target_is_directory=True)
            self.bad("output_unsafe", lambda: c.PrivateOutput(str(root / "link" / "new")))
            public = root / "public"; public.mkdir(mode=0o755); public.chmod(0o755)
            self.bad("output_unsafe", lambda: c.PrivateOutput(str(public / "new")))

    def test_config_reads_only_auth_and_registry(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / "config"
            p.write_text("DASHSCOPE_API_KEY=ignored\nROBOT_ALLOWED_DEVICE=" + MAC + "\nROBOT_DEVICE_TOKEN='" + TOKEN + "'\n")
            p.chmod(0o600)
            self.assertEqual(c.load_token(str(p), MAC), TOKEN)
            self.bad("auth_config", lambda: c.load_token(str(p), "11:22:33:44:55:66"))
            p.chmod(0o644)
            self.bad("auth_config", lambda: c.load_token(str(p), MAC))

    def test_libopus_version_is_fixed_safe_text(self):
        decoder = c.OpusDecoder()
        try:
            self.assertTrue(hasattr(decoder, "version"), "decoder must record native version")
            self.assertRegex(decoder.version, r"^libopus [0-9][0-9A-Za-z. +_-]{0,63}$")
        finally:
            decoder.close()

    def test_root_readable_service_registry_and_group_read_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            registry = root / "registry.json"
            registry.write_text(json.dumps({"schema_version": 1, "devices": [{"mac": MAC, "token": TOKEN}, {"mac": "11:22:33:44:55:66", "token": "s" * 64}]}))
            registry.chmod(0o600)
            if os.geteuid() == 0:
                os.chown(registry, 999, 999)
            config = root / "config"
            config.write_text("ROBOT_ALLOWED_DEVICE=" + MAC + "\nROBOT_DEVICE_TOKEN=" + TOKEN + "\nROBOT_DEVICE_REGISTRY=" + str(registry) + "\n")
            config.chmod(0o640)
            self.assertEqual(c.load_token(str(config), "11:22:33:44:55:66"), "s" * 64)
            config.chmod(0o660)
            self.bad("auth_config", lambda: c.load_token(str(config), MAC))


class LifecycleTests(Base):
    def engine(self):
        return c.Engine(MAC, TOKEN, lambda p: b"\x01\x00" * 960)

    def process(self, script):
        children = []
        def popen(argv, **kwargs):
            self.assertEqual(argv[:3], ["tcpdump", "-n", "-i"])
            process = subprocess.Popen([sys.executable, "-u", "-c", script], **kwargs)
            children.append(process)
            return process
        return children, popen

    def test_timeout_partial_header_is_bounded_and_child_reaped(self):
        children, popen = self.process("import os,time;os.write(1,b'partial');time.sleep(30)")
        ready = []
        before = time.monotonic()
        error, verified, drops = c.capture_live(self.engine(), 0.15, popen=popen, ready=lambda: ready.append(True))
        self.assertEqual(error, "timeout")
        self.assertFalse(verified)
        self.assertIsNone(drops)
        self.assertEqual(ready, [])
        self.assertIsNotNone(children[0].returncode)
        self.assertLess(time.monotonic() - before, 3)

    def test_ready_requires_actual_listening_stderr_then_zero_stats(self):
        script = "import os,signal,time\ndef stop(a,b):\n os.write(2,b'0 packets dropped by kernel\\n');raise SystemExit(0)\nsignal.signal(signal.SIGTERM,stop)\nos.write(2,b'tcpdump: listening on eth0, link-type EN10MB\\n')\ntime.sleep(30)"
        children, popen = self.process(script)
        ready = []
        error, verified, drops = c.capture_live(self.engine(), 0.15, popen=popen, ready=lambda: ready.append(True))
        self.assertEqual(ready, [True])
        self.assertEqual(error, "timeout")
        self.assertEqual((verified, drops), (True, 0))
        self.assertEqual(children[0].returncode, 0)

    def test_nonzero_exit_and_unavailable_stats_invalidate(self):
        raw = pcap([])
        children, popen = self.process("import os;os.write(1," + repr(raw) + ");raise SystemExit(3)")
        error, verified, _ = c.capture_live(self.engine(), 0.5, popen=popen)
        self.assertEqual(error, "capture_exit")
        self.assertFalse(verified)
        self.assertEqual(children[0].returncode, 3)

    def test_term_ignoring_child_is_killed_reaped(self):
        child = subprocess.Popen([sys.executable, "-u", "-c", "import signal,time;signal.signal(signal.SIGTERM,signal.SIG_IGN);print('armed',flush=True);time.sleep(30)"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.assertEqual(child.stdout.readline(), b"armed\n")
        c.stop_child(child, timeout=0.05)
        self.assertEqual(child.returncode, -9)
        child.stdout.close(); child.stderr.close()

    def test_missing_or_nonzero_drop_stats_remove_otherwise_valid_wav(self):
        for verified, drops, reason in [(False, None, "capture_stats"), (True, 1, "capture_drops")]:
            with tempfile.TemporaryDirectory() as tmp:
                s = c.Session(MAC, TOKEN, lambda p: b"")
                s.complete = True; s.reason = "tts_start"; s.frames = [b"x"]; s.pcm = bytearray(b"\x01\x00" * 960)
                path = Path(tmp) / "sample"
                result = c.PrivateOutput(str(path)).finish(s, None, verified, drops)
                self.assertEqual(result["status"], "incomplete")
                self.assertEqual(result["reason"], reason)
                self.assertFalse((path / "utterance.wav").exists())

    def test_complete_capture_requires_readiness_confirmation(self):
        raw = complete_pcap()
        for confirmed in (False, True):
            script = "import os,signal,time\ndef stop(a,b):\n os.write(2,b'0 packets dropped by kernel\\n');raise SystemExit(0)\nsignal.signal(signal.SIGTERM,stop)\n"
            if confirmed:
                script += "os.write(2,b'tcpdump: listening on eth0, link-type EN10MB\\n')\ntime.sleep(0.03)\n"
            script += "os.write(1," + repr(raw) + ")\ntime.sleep(30)"
            children, popen = self.process(script)
            ready = []
            engine = self.engine()
            error, verified, drops = c.capture_live(engine, 0.5, popen=popen, ready=lambda: ready.append(True))
            self.assertTrue(engine.session.complete)
            self.assertEqual(engine.session.frames, [b"business"])
            self.assertEqual(error, None if confirmed else "capture_start")
            self.assertEqual(ready, [True] if confirmed else [])
            self.assertEqual((verified, drops), (True, 0))
            self.assertEqual(children[0].returncode, 0)

    def test_term_and_hup_recorder_cleanup_is_bounded_and_sanitized(self):
        for sig in (signal.SIGTERM, signal.SIGHUP):
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                config = root / "config"
                config.write_text("ROBOT_ALLOWED_DEVICE=" + MAC + "\nROBOT_DEVICE_TOKEN=" + TOKEN + "\n")
                config.chmod(0o600)
                child_pid_file = root / "child.pid"
                sample = root / "sample"
                fixture = "import os,signal,time\ndef stop(a,b):\n os.write(2,b'0 packets dropped by kernel\\n');raise SystemExit(0)\nsignal.signal(signal.SIGTERM,stop)\nos.write(2,b'tcpdump: listening on eth0, link-type EN10MB\\n')\ntime.sleep(30)"
                script = "import capture as c,subprocess,sys\nfrom pathlib import Path\nactual=subprocess.Popen\nrun=c.capture_live\ndef fake(argv,**kw):\n child=actual([sys.executable,'-u','-c'," + repr(fixture) + "],**kw)\n Path(" + repr(str(child_pid_file)) + ").write_text(str(child.pid))\n return child\nc.capture_live=lambda engine,wait,ready=None:run(engine,wait,popen=fake,ready=ready)\nraise SystemExit(c.main(" + repr(["--device", MAC, "--config", str(config), "--output", str(sample), "--wait-limit", "10"]) + "))"
                recorder = subprocess.Popen([sys.executable, "-u", "-c", script], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                child_pid = None
                try:
                    self.assertTrue(select.select([recorder.stdout], [], [], 3)[0], "recorder must arm fixture")
                    self.assertEqual(json.loads(recorder.stdout.readline())["event"], "ready")
                    child_pid = int(child_pid_file.read_text())
                    recorder.send_signal(sig)
                    stdout, stderr = recorder.communicate(timeout=4)
                    self.assertEqual(recorder.returncode, 2)
                    self.assertEqual(stderr, b"")
                    summary = json.loads(stdout)
                    self.assertEqual(summary["reason"], "signal")
                    self.assertEqual(summary["status"], "incomplete")
                    self.assertEqual(json.loads((sample / "metadata.json").read_text())["reason"], "signal")
                    self.assertFalse((sample / "utterance.wav").exists())
                    with self.assertRaises(ProcessLookupError): os.kill(child_pid, 0)
                finally:
                    if recorder.poll() is None:
                        recorder.kill(); recorder.communicate(timeout=2)
                    if child_pid:
                        try: os.kill(child_pid, signal.SIGKILL)
                        except ProcessLookupError: pass
                    recorder.stdout.close(); recorder.stderr.close()


if __name__ == "__main__":
    unittest.main()

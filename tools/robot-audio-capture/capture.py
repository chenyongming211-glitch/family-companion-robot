#!/usr/bin/env python3
"""Bounded, passive first-utterance Opus capture. Run on the cloud only.

No pcap, HTTP headers, credentials, transcripts, or downlink audio are written.
Protocol bytes exist only in bounded process memory. See README.md for limits.
"""
import argparse
import base64
import ctypes
import hashlib
import hmac
import io
import json
import math
import os
from pathlib import Path
import re
import selectors
import signal
import stat
import struct
import subprocess
import sys
import time
import wave
from dataclasses import dataclass

MAX_PCAP = 16 * 1024 * 1024
MAX_STREAM = 1024 * 1024
MAX_HTTP = 16384
MAX_WS = 65536
MAX_FLOWS = 32
PORT = 18041
REASONS = frozenset("pcap_limit pcap_header pcap_truncated packet_header ip_fragment tcp_header tcp_gap tcp_conflict tcp_limit tcp_handshake tcp_reset flow_limit http_header http_upgrade ws_mask ws_control ws_fragment ws_size ws_header ws_close hello_format session_scope listen_state opus_packet opus_decode opus_samples wake_limit audio_limit empty_audio boundary_partial client_stop client_fin tts_start auth_config output_unsafe timeout first_audio_timeout capture_start capture_io capture_exit capture_drops capture_stats signal internal_error".split())


class CaptureError(Exception):
    def __init__(self, reason):
        self.reason = reason if reason in REASONS else "internal_error"
        super().__init__(self.reason)


def require(ok, reason):
    if not ok:
        raise CaptureError(reason)


def json_object(raw, reason):
    def unique(pairs):
        obj = {}
        for key, value in pairs:
            require(key not in obj, reason)
            obj[key] = value
        return obj
    try:
        value = json.loads(raw, object_pairs_hook=unique)
        require(isinstance(value, dict), reason)
        return value
    except (ValueError, TypeError, UnicodeError, RecursionError):
        raise CaptureError(reason) from None


def canonical_mac(value):
    require(isinstance(value, str) and re.fullmatch(r"(?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}", value), "auth_config")
    return value.lower()


def private_read(path, limit):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        s = os.fstat(fd)
        require(stat.S_ISREG(s.st_mode) and s.st_mode & 0o137 == 0 and (os.geteuid() == 0 or s.st_uid == os.geteuid()) and s.st_size <= limit, "auth_config")
        data = os.read(fd, limit + 1)
        require(len(data) <= limit, "auth_config")
        return data
    finally:
        os.close(fd)


def load_token(config, device):
    """Do not evaluate shell syntax, source the env file, or read other keys."""
    try:
        device = canonical_mac(device)
        keys = {}
        for line in private_read(config, 1024 * 1024).decode().splitlines():
            key, sep, value = line.strip().partition("=")
            if sep and key in {"ROBOT_DEVICE_TOKEN", "ROBOT_ALLOWED_DEVICE", "ROBOT_DEVICE_REGISTRY"}:
                require(key not in keys, "auth_config")
                value = value.strip()
                if value[:1] in {"'", '"'}:
                    require(len(value) >= 2 and value[-1] == value[0], "auth_config")
                    value = value[1:-1]
                keys[key] = value
        primary_mac = canonical_mac(keys.get("ROBOT_ALLOWED_DEVICE", ""))
        primary_token = keys.get("ROBOT_DEVICE_TOKEN", "")
        def valid(token):
            return isinstance(token, str) and 32 <= len(token) <= 512 and not re.search(r"[\x00\s]", token)
        require(valid(primary_token), "auth_config")
        if keys.get("ROBOT_DEVICE_REGISTRY"):
            registry = json_object(private_read(keys["ROBOT_DEVICE_REGISTRY"], 1024 * 1024), "auth_config")
            entries = registry.get("devices")
            require(registry.get("schema_version") == 1 and isinstance(entries, list) and 1 <= len(entries) <= 64, "auth_config")
            tokens, seen = {}, set()
            for entry in entries:
                require(isinstance(entry, dict), "auth_config")
                mac, token = canonical_mac(entry.get("mac")), entry.get("token")
                require(mac not in tokens and valid(token) and token not in seen, "auth_config")
                tokens[mac] = token
                seen.add(token)
            require(tokens.get(primary_mac) == primary_token and device in tokens, "auth_config")
            return tokens[device]
        require(device == primary_mac, "auth_config")
        return primary_token
    except (OSError, UnicodeError, ValueError, TypeError):
        raise CaptureError("auth_config") from None


@dataclass
class Packet:
    key: tuple
    client: bool
    seq: int
    ack: int
    flags: int
    payload: bytes


class PcapStream:
    def __init__(self, limit=MAX_PCAP):
        self.buf = bytearray()
        self.endian = None
        self.total = 0
        self.limit = limit
        self.snaplen = None

    def feed(self, data):
        self.total += len(data)
        require(self.total <= self.limit, "pcap_limit")
        self.buf.extend(data)
        if self.endian is None:
            if len(self.buf) < 24:
                return
            magic = bytes(self.buf[:4])
            require(magic in {b"\xd4\xc3\xb2\xa1", b"\xa1\xb2\xc3\xd4", b"\x4d\x3c\xb2\xa1", b"\xa1\xb2\x3c\x4d"}, "pcap_header")
            self.endian = "<" if magic in {b"\xd4\xc3\xb2\xa1", b"\x4d\x3c\xb2\xa1"} else ">"
            major, minor, _, _, self.snaplen, link = struct.unpack(self.endian + "HHIIII", self.buf[4:24])
            require((major, minor, link) == (2, 4, 1) and 54 <= self.snaplen <= 262144, "pcap_header")
            del self.buf[:24]
        while len(self.buf) >= 16:
            _, _, included, original = struct.unpack(self.endian + "IIII", self.buf[:16])
            require(included == original, "pcap_truncated")
            require(14 <= included <= self.snaplen, "packet_header")
            if len(self.buf) < 16 + included:
                return
            raw = bytes(self.buf[16:16 + included])
            del self.buf[:16 + included]
            packet = self.packet(raw)
            if packet is not None:
                yield packet

    def finish(self):
        require(self.endian is not None and not self.buf, "pcap_truncated")

    @staticmethod
    def packet(raw):
        require(len(raw) >= 14, "packet_header")
        if raw[12:14] != b"\x08\x00":
            return None
        ip = raw[14:]
        require(len(ip) >= 20 and ip[0] >> 4 == 4, "packet_header")
        hlen = (ip[0] & 15) * 4
        size = int.from_bytes(ip[2:4], "big")
        require(hlen >= 20 and hlen <= size <= len(ip), "packet_header")
        if ip[9] != 6:
            return None
        require(int.from_bytes(ip[6:8], "big") & 0x3fff == 0, "ip_fragment")
        tcp = ip[hlen:size]
        require(len(tcp) >= 20, "tcp_header")
        sport, dport, seq, ack = struct.unpack("!HHII", tcp[:12])
        if PORT not in {sport, dport}:
            return None
        offset = (tcp[12] >> 4) * 4
        require(20 <= offset <= len(tcp), "tcp_header")
        client = dport == PORT
        src, dst = bytes(ip[12:16]), bytes(ip[16:20])
        key = (src, sport, dst, dport) if client else (dst, dport, src, sport)
        # Offload can leave checksums incomplete at this capture location.
        return Packet(key, client, seq, ack, tcp[13], bytes(tcp[offset:]))


class TCPStream:
    """Strict capture order: accept exact retransmits, reject holes/conflicts."""
    def __init__(self, start, limit=MAX_STREAM):
        self.start = start & 0xffffffff
        self.history = bytearray()
        self.limit = limit

    @property
    def next(self):
        return (self.start + len(self.history)) & 0xffffffff

    def feed(self, seq, data):
        offset = (seq - self.start) & 0xffffffff
        require(offset <= len(self.history), "tcp_gap")
        overlap = min(len(data), len(self.history) - offset)
        require(self.history[offset:offset + overlap] == data[:overlap], "tcp_conflict")
        suffix = data[overlap:]
        require(len(self.history) + len(suffix) <= self.limit, "tcp_limit")
        self.history.extend(suffix)
        return suffix


class WebSocket:
    def __init__(self, client):
        self.client = client
        self.buf = bytearray()
        self.fragment = None
        self.parts = bytearray()

    def idle(self):
        return not self.buf and self.fragment is None

    def feed(self, data):
        self.buf.extend(data)
        require(len(self.buf) <= MAX_STREAM, "ws_size")
        while len(self.buf) >= 2:
            a, b = self.buf[:2]
            fin, op, masked = bool(a & 128), a & 15, bool(b & 128)
            require(a & 0x70 == 0 and op in {0, 1, 2, 8, 9, 10}, "ws_header")
            require(masked == self.client, "ws_mask")
            length, pos = b & 127, 2
            if length in {126, 127}:
                n = 2 if length == 126 else 8
                if len(self.buf) < pos + n:
                    return
                value = int.from_bytes(self.buf[pos:pos + n], "big")
                require(value >= (126 if n == 2 else 65536) and value < 1 << 63, "ws_header")
                length, pos = value, pos + n
            if op >= 8:
                require(fin and length <= 125, "ws_control")
            else:
                kind = self.fragment if op == 0 else op
                cap = 4096 if kind == 2 else MAX_WS
                require(length + (len(self.parts) if op == 0 else 0) <= cap, "ws_size")
            if masked:
                if len(self.buf) < pos + 4:
                    return
                mask = self.buf[pos:pos + 4]
                pos += 4
            if len(self.buf) < pos + length:
                return
            payload = bytes(self.buf[pos:pos + length])
            del self.buf[:pos + length]
            if masked:
                payload = bytes(v ^ mask[i % 4] for i, v in enumerate(payload))
            if op >= 8:
                if op == 8:
                    require(len(payload) != 1, "ws_close")
                yield op, payload
                continue
            if op == 0:
                require(self.fragment is not None, "ws_fragment")
                self.parts.extend(payload)
                if not fin:
                    continue
                op, payload = self.fragment, bytes(self.parts)
                self.fragment = None
                self.parts.clear()
            else:
                require(self.fragment is None, "ws_fragment")
                if not fin:
                    self.fragment = op
                    self.parts.extend(payload)
                    continue
            yield op, payload


def http_headers(raw):
    try:
        lines = raw.decode("ascii").split("\r\n")
        require(lines[-2:] == ["", ""], "http_header")
        headers = {}
        for line in lines[1:-2]:
            key, sep, value = line.partition(":")
            require(sep and re.fullmatch(r"[!#$%&'*+.^_`|~0-9a-zA-Z-]+", key) and key.lower() not in headers and "\x00" not in value, "http_header")
            headers[key.lower()] = value.strip()
        return lines[0], headers
    except (ValueError, UnicodeError):
        raise CaptureError("http_header") from None


class Session:
    def __init__(self, mac, token, decoder, audio_limit=15):
        self.mac, self.token, self.decoder = mac, token, decoder
        self.audio_limit = audio_limit
        self.http = {True: bytearray(), False: bytearray()}
        self.ws = {True: WebSocket(True), False: WebSocket(False)}
        self.authenticated = self.upgraded = self.rejected = False
        self.client_hello = False
        self.scope = None
        self.key = None
        self.listening = False
        self.listen_at = self.first_audio_at = None
        self.frames = []
        self.pcm = bytearray()
        self.wake_discarded_frames = 0
        self.wake_discarded_samples = 0
        self.complete = False
        self.reason = None

    def feed(self, client, data):
        if self.complete or self.rejected:
            return
        if not self.upgraded:
            require(client or self.authenticated, "http_upgrade")
            buf = self.http[client]
            buf.extend(data)
            end = buf.find(b"\r\n\r\n")
            require((end + 4 if end >= 0 else len(buf)) <= MAX_HTTP, "http_header")
            if end < 0:
                return
            first, headers = http_headers(bytes(buf[:end + 4]))
            extra = bytes(buf[end + 4:])
            buf.clear()
            if client:
                require(not self.authenticated and first == "GET /xiaozhi/v1/ HTTP/1.1", "http_upgrade")
                self.rejected = headers.get("device-id", "").lower() != self.mac or not hmac.compare_digest(headers.get("authorization", ""), "Bearer " + self.token)
                if self.rejected:
                    self.token = ""
                    return
                require(headers.get("protocol-version") == "1" and headers.get("sec-websocket-version") == "13", "http_upgrade")
                require(headers.get("upgrade", "").lower() == "websocket" and "upgrade" in {x.strip().lower() for x in headers.get("connection", "").split(",")}, "http_upgrade")
                self.key = headers.get("sec-websocket-key", "")
                try:
                    require(len(base64.b64decode(self.key, validate=True)) == 16, "http_upgrade")
                except ValueError:
                    raise CaptureError("http_upgrade") from None
                require(not extra, "http_upgrade")
                self.authenticated = True
                self.token = ""
                return
            accept = base64.b64encode(hashlib.sha1((self.key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
            require(first.startswith("HTTP/1.1 101 ") and headers.get("sec-websocket-accept") == accept and headers.get("upgrade", "").lower() == "websocket" and "upgrade" in {x.strip().lower() for x in headers.get("connection", "").split(",")}, "http_upgrade")
            self.upgraded = True
            self.key = None
            data = extra
        for op, payload in self.ws[client].feed(data):
            if self.complete:
                break
            if op == 8:
                raise CaptureError("ws_close")
            if op in {9, 10}:
                continue
            if op == 2:
                if not client:
                    continue  # Never persist or decode model audio.
                require(self.client_hello, "hello_format")
                require(self.scope is not None, "session_scope")
                require(0 < len(payload) <= 4096, "opus_packet")
                if not self.listening:
                    require(self.wake_discarded_samples + 960 <= 32000, "wake_limit")
                else:
                    require(len(self.frames) * 0.06 + 0.06 <= self.audio_limit + 1e-9, "audio_limit")
                pcm = self.decoder(payload)
                require(len(pcm) == 1920, "opus_samples")
                if not self.listening:
                    # Match the backend's valid packet decode order while
                    # retaining no wake packet or PCM in capture artifacts.
                    self.wake_discarded_frames += 1
                    self.wake_discarded_samples += 960
                    del pcm
                    continue
                self.frames.append(payload)
                self.pcm.extend(pcm)
                if self.first_audio_at is None:
                    self.first_audio_at = time.monotonic()
                continue
            event = json_object(payload, "hello_format")
            if client and not self.client_hello:
                self.validate_hello(event, 16000)
                self.client_hello = True
                continue
            if not client and self.scope is None:
                require(self.client_hello, "hello_format")
                self.validate_hello(event, 24000)
                scope = event.get("session_id")
                require(isinstance(scope, str) and 1 <= len(scope) <= 128, "session_scope")
                self.scope = scope
                continue
            require(event.get("type") != "hello", "hello_format")
            kind = event.get("type")
            if kind in {"listen", "tts", "abort", "stop", "interrupt"}:
                require(self.scope is not None and event.get("session_id") == self.scope, "session_scope")
            elif "session_id" in event and event["session_id"] not in {"", self.scope}:
                raise CaptureError("session_scope")
            if client and kind == "listen":
                state = event.get("state")
                if state == "start":
                    require(not self.listening and event.get("mode") in {"realtime", "auto"}, "listen_state")
                    self.listening = True
                    self.listen_at = time.monotonic()
                elif state == "stop":
                    require(self.listening, "listen_state")
                    self.boundary("client_stop", opposite=False)
                else:
                    require(state == "detect" and not self.listening, "listen_state")
            elif not client and kind == "tts" and event.get("state") == "start":
                require(self.listening, "listen_state")
                self.boundary("tts_start", opposite=True)
            elif client and kind == "abort" and not self.listening:
                continue  # Normal wake abort has no business audio authority.
            elif client and kind in {"abort", "stop", "interrupt"}:
                raise CaptureError("listen_state")

    @staticmethod
    def validate_hello(event, rate):
        params = event.get("audio_params")
        require(event.get("type") == "hello" and event.get("version") == 1 and event.get("transport") == "websocket" and isinstance(params, dict) and all(params.get(k) == v for k, v in {"format": "opus", "sample_rate": rate, "channels": 1, "frame_duration": 60}.items()), "hello_format")

    def boundary(self, reason, opposite):
        require(self.frames, "empty_audio")
        # Opposite direction may already have an unfinished WS message. Never
        # reorder packets after a downlink boundary to manufacture completeness.
        require(self.ws[opposite].idle(), "boundary_partial")
        self.reason, self.complete = reason, True

    def fin(self, client):
        require(client and self.listening and self.ws[True].idle() and self.ws[False].idle(), "boundary_partial")
        self.boundary("client_fin", opposite=False)


class Flow:
    def __init__(self, packet, mac, token, decoder, audio_limit):
        require(packet.flags == 2 and not packet.payload, "tcp_handshake")
        self.streams = {True: TCPStream(packet.seq + 1)}
        self.syn = packet.seq
        self.state = 1
        self.session = Session(mac, token, decoder, audio_limit)

    def feed(self, p):
        if self.state == 1:
            if p.client and p.flags == 2 and p.seq == self.syn and not p.payload:
                return
            require(not p.client and p.flags == 0x12 and p.ack == self.streams[True].next and not p.payload, "tcp_handshake")
            self.streams[False] = TCPStream(p.seq + 1)
            self.state = 2
            return
        if p.flags & 2:
            require(not p.client and p.flags == 0x12 and p.seq == (self.streams[False].start - 1) & 0xffffffff and p.ack == self.streams[True].start and not p.payload, "tcp_handshake")
            return
        if self.state == 2:
            require(p.client and p.flags & 0x10 and p.seq == self.streams[True].next and p.ack == self.streams[False].next, "tcp_handshake")
            self.state = 3
        require(not p.flags & 4, "tcp_reset")
        require(p.flags & 0x10 and not p.flags & 0xc0, "tcp_header")
        # An ACK beyond the last observed peer byte proves that capture missed
        # peer data, even if no later peer segment exposes a sequence gap.
        # RFC serial arithmetic allows delayed ACKs across 32-bit wraparound.
        ahead = (p.ack - self.streams[not p.client].next) & 0xffffffff
        require(ahead == 0 or ahead > 0x80000000, "tcp_gap")
        data = self.streams[p.client].feed(p.seq, p.payload)
        if data:
            self.session.feed(p.client, data)
        if p.flags & 1 and not self.session.complete:
            require((p.seq + len(p.payload)) & 0xffffffff == self.streams[p.client].next, "tcp_gap")
            self.session.fin(p.client)


class Engine:
    def __init__(self, mac, token, decoder, audio_limit=15):
        self.mac, self.token, self.decoder, self.audio_limit = mac, token, decoder, audio_limit
        self.flows = {}
        self.selected = None
        self.seen = 0

    def feed(self, packet):
        if self.selected is not None and packet.key != self.selected:
            return
        flow = self.flows.get(packet.key)
        if flow is None:
            if not (packet.client and packet.flags == 2 and not packet.payload):
                return
            self.seen += 1
            require(self.seen <= MAX_FLOWS, "flow_limit")
            self.flows[packet.key] = Flow(packet, self.mac, self.token, self.decoder, self.audio_limit)
            return
        try:
            flow.feed(packet)
        except CaptureError:
            if self.selected == packet.key or flow.session.authenticated:
                self.selected = packet.key
                raise
            del self.flows[packet.key]
            return
        if flow.session.rejected:
            del self.flows[packet.key]
        elif flow.session.authenticated and self.selected is None:
            self.selected = packet.key
            self.flows = {packet.key: flow}
            self.token = ""

    @property
    def session(self):
        return self.flows[self.selected].session if self.selected in self.flows else None


class OpusDecoder:
    def __init__(self):
        self.state = None
        try:
            self.lib = ctypes.CDLL("libopus.so.0")
            self.lib.opus_get_version_string.restype = ctypes.c_char_p
            version = self.lib.opus_get_version_string()
            require(version is not None and re.fullmatch(rb"libopus [0-9][0-9A-Za-z. +_-]{0,63}", version), "opus_decode")
            self.version = version.decode("ascii")
            self.lib.opus_decoder_create.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.POINTER(ctypes.c_int)]
            self.lib.opus_decoder_create.restype = ctypes.c_void_p
            self.lib.opus_decode.argtypes = [ctypes.c_void_p, ctypes.c_void_p, ctypes.c_int32, ctypes.POINTER(ctypes.c_int16), ctypes.c_int, ctypes.c_int]
            self.lib.opus_decode.restype = ctypes.c_int
            self.lib.opus_decoder_destroy.argtypes = [ctypes.c_void_p]
            error = ctypes.c_int()
            self.state = self.lib.opus_decoder_create(16000, 1, ctypes.byref(error))
            require(self.state and error.value == 0, "opus_decode")
        except OSError:
            raise CaptureError("opus_decode") from None

    def decode(self, packet):
        require(0 < len(packet) <= 4096, "opus_packet")
        output = (ctypes.c_int16 * 960)()
        n = self.lib.opus_decode(self.state, packet, len(packet), output, 960, 0)
        require(n >= 0, "opus_decode")
        require(n == 960, "opus_samples")
        # Portable explicit little-endian WAV samples; never PLC/FEC.
        return struct.pack("<960h", *output)

    def close(self):
        if self.state:
            self.lib.opus_decoder_destroy(self.state)
            self.state = None


class PrivateOutput:
    def __init__(self, path):
        self.fd = None
        try:
            p = Path(path)
            require(p.is_absolute() and ".." not in p.parts and p.name not in {"", "."}, "output_unsafe")
            # Open every ancestor without following symlinks. The immediate
            # parent must already be private; higher parents may be system dirs.
            fd = os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
            try:
                for part in p.parts[1:-1]:
                    nxt = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
                    os.close(fd); fd = nxt
                parent = os.fstat(fd)
                require(parent.st_uid == os.geteuid() and parent.st_mode & 0o077 == 0, "output_unsafe")
                os.mkdir(p.name, mode=0o700, dir_fd=fd)
                self.fd = os.open(p.name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
                require(stat.S_IMODE(os.fstat(self.fd).st_mode) == 0o700, "output_unsafe")
            finally:
                os.close(fd)
        except (OSError, ValueError):
            raise CaptureError("output_unsafe") from None

    def write(self, name, raw):
        fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=self.fd)
        try:
            os.fchmod(fd, 0o600)
            with os.fdopen(fd, "wb", closefd=False) as stream:
                stream.write(raw)
                stream.flush()
                os.fsync(fd)
        finally:
            os.close(fd)

    def finish(self, session, error, verified, drops):
        try:
            good = bool(session and session.complete and not error and verified and drops == 0)
            reason = error or (session.reason if session and session.complete else "timeout")
            if not verified:
                good, reason = False, "capture_stats"
            elif drops != 0:
                good, reason = False, "capture_drops"
            frames = session.frames if session else []
            pcm = bytes(session.pcm) if session else b""
            source = b"ROBOTOPUS1\n" + b"".join(struct.pack("!I", len(frame)) + frame for frame in frames)
            self.write("utterance.opusframes", source)
            wav = None
            if good:
                buffer = io.BytesIO()
                with wave.open(buffer, "wb") as f:
                    f.setnchannels(1); f.setsampwidth(2); f.setframerate(16000); f.writeframes(pcm)
                wav = buffer.getvalue()
                self.write("utterance.wav", wav)
            samples = struct.unpack("<" + "h" * (len(pcm) // 2), pcm)
            decoder_version = getattr(getattr(session.decoder, "__self__", None), "version", None) if session else None
            report = {"schema_version": 1, "status": "complete" if good else "incomplete", "reason": reason if reason in REASONS else "internal_error", "stage": "capture_readiness", "source_kind": "private_human", "source_device": "waveshare_esp32_s3", "capture_scope": "first_business_input", "decoder_version": decoder_version, "source_format": {"codec": "opus", "protocol_version": 1, "sample_rate": 16000, "channels": 1, "frame_duration_ms": 60}, "human_acceptance": False, "production_acceptance": False, "frames": len(frames), "samples": len(samples), "duration_seconds": len(samples) / 16000, "opus_payload_bytes": sum(map(len, frames)), "source_bytes": len(source), "wav_bytes": len(wav) if wav else 0, "rms": math.sqrt(sum(x * x for x in samples) / len(samples)) if samples else 0, "peak": max(map(abs, samples), default=0), "source_sha256": hashlib.sha256(source).hexdigest(), "wav_sha256": hashlib.sha256(wav).hexdigest() if wav else None, "drop_stats_verified": bool(verified), "kernel_drops": drops}
            report["source_device_basis"] = "user_declared_and_configured_target"
            report["session_sha256"] = hashlib.sha256(session.scope.encode()).hexdigest() if session and session.scope else None
            report["decoder_policy"] = "bounded_prelisten_warmup"
            report["context_saved"] = False
            report["source_only_redecode_is_server_pcm"] = False
            report["wake_discarded_frames"] = session.wake_discarded_frames if session else 0
            report["wake_discarded_samples"] = session.wake_discarded_samples if session else 0
            self.write("metadata.json", (json.dumps(report, sort_keys=True, indent=2) + "\n").encode())
            os.fsync(self.fd)
            return report
        finally:
            os.close(self.fd)
            self.fd = None


def drop_status(stderr):
    values = re.findall(rb"(?m)^([0-9]+) packets? dropped by kernel\r?$", stderr)
    return (True, int(values[0])) if len(values) == 1 else (False, None)


def stop_child(child, timeout=2):
    """Bounded termination; caller has already drained normal pipe traffic."""
    if child.poll() is None:
        child.terminate()
    try:
        return child.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        child.kill()
        return child.communicate(timeout=timeout)


def capture_live(engine, wait_limit, first_audio_limit=15, popen=subprocess.Popen, ready=None):
    """The only live entry. Tests call parsers directly and never invoke it."""
    child = None
    selector = selectors.DefaultSelector()
    stderr = bytearray()
    error = None
    parser = PcapStream()
    started = time.monotonic()
    listening = False
    previous_handlers = {}
    def stop_signal(signum, frame):
        raise CaptureError("signal")
    try:
        for signum in (signal.SIGTERM, signal.SIGHUP):
            previous_handlers[signum] = signal.signal(signum, stop_signal)
        child = popen(["tcpdump", "-n", "-i", "eth0", "-s", "0", "-U", "-B", "4096", "-w", "-", "tcp", "port", str(PORT)], stdout=subprocess.PIPE, stderr=subprocess.PIPE, stdin=subprocess.DEVNULL, env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C"}, start_new_session=True)
        for stream in (child.stdout, child.stderr):
            os.set_blocking(stream.fileno(), False)
            selector.register(stream, selectors.EVENT_READ)
        while True:
            now = time.monotonic()
            require(now - started < min(wait_limit, 176), "timeout")
            require(listening or now - started < min(5, wait_limit), "capture_start")
            session = engine.session
            if session and session.listen_at and not session.frames:
                require(now - session.listen_at < first_audio_limit, "first_audio_timeout")
            if session and session.first_audio_at:
                require(now - session.first_audio_at <= engine.audio_limit + 1, "audio_limit")
            for key, _ in selector.select(timeout=min(0.2, wait_limit - (now - started))):
                data = os.read(key.fd, 65536)
                if not data:
                    selector.unregister(key.fileobj)
                    continue
                if key.fileobj is child.stderr:
                    require(len(stderr) + len(data) <= 65536, "capture_io")
                    stderr.extend(data)
                    if not listening and re.search(rb"(?m)^tcpdump: listening on eth0,", stderr):
                        listening = True
                        if ready:
                            ready()
                else:
                    for packet in parser.feed(data):
                        engine.feed(packet)
                        if engine.session and engine.session.complete:
                            break
            if engine.session and engine.session.complete:
                require(listening, "capture_start")
                break
            if child.poll() is not None or not selector.get_map():
                parser.finish()
                raise CaptureError("capture_exit")
    except CaptureError as e:
        error = e.reason
    except (KeyboardInterrupt, InterruptedError):
        error = "signal"
    except (OSError, ValueError, subprocess.SubprocessError):
        error = "capture_io"
    finally:
        # A second TERM/HUP must not interrupt the bounded cleanup itself.
        for signum in previous_handlers:
            signal.signal(signum, signal.SIG_IGN)
        selector.close()
        if child:
            try:
                # communicate drains trailing bytes into memory only, bounded by
                # tcpdump's stopped pipes; never parse traffic after boundary.
                _, tail = stop_child(child)
                require(len(stderr) + len(tail) <= 65536, "capture_io")
                stderr.extend(tail)
                if child.returncode not in {0, -signal.SIGTERM} and error is None:
                    error = "capture_exit"
            except (OSError, subprocess.SubprocessError, CaptureError):
                error = "capture_io"
            finally:
                child.stdout.close(); child.stderr.close()
        for signum, handler in previous_handlers.items():
            signal.signal(signum, handler)
    verified, drops = drop_status(bytes(stderr))
    return error, verified, drops


def main(argv=None):
    parser = argparse.ArgumentParser(description="Cloud-only private passive first business utterance capture")
    parser.add_argument("--device", required=True)
    parser.add_argument("--config", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--wait-limit", type=float, default=180)
    parser.add_argument("--audio-limit", type=float, default=15)
    args = parser.parse_args(argv)
    decoder = None
    output = None
    engine = None
    try:
        require(0 < args.wait_limit <= 180 and 0 < args.audio_limit <= 15, "audio_limit")
        mac = canonical_mac(args.device)
        token = load_token(args.config, mac)
        output = PrivateOutput(args.output)
        decoder = OpusDecoder()
        engine = Engine(mac, token, decoder.decode, args.audio_limit)
        def ready():
            print(json.dumps({"event": "ready", "stage": "capture_readiness"}), flush=True)
        error, verified, drops = capture_live(engine, args.wait_limit, ready=ready)
        report = output.finish(engine.session, error, verified, drops)
        print(json.dumps({"event": "finished", "status": report["status"], "reason": report["reason"], "frames": report["frames"], "samples": report["samples"], "stage": "capture_readiness", "human_acceptance": False, "production_acceptance": False}), flush=True)
        return 0 if report["status"] == "complete" else 2
    except (CaptureError, OSError, ValueError, TypeError) as e:
        reason = e.reason if isinstance(e, CaptureError) else "internal_error"
        if output and output.fd is not None:
            try:
                output.finish(engine.session if engine else None, reason, False, None)
            except (OSError, CaptureError):
                pass
        print(json.dumps({"event": "finished", "status": "incomplete", "reason": reason, "stage": "capture_readiness", "human_acceptance": False, "production_acceptance": False}), flush=True)
        return 2
    finally:
        if decoder:
            decoder.close()


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
"""Bounded stop-and-wait USB PCM audition client; no microphone input."""
import argparse
import signal
import struct
import time
import wave
import zlib
from pathlib import Path

HEADER = struct.Struct('<4sIIIII')
MAX_PAYLOAD = 2880


def _validate(op, seq, arg, payload):
    if not all(isinstance(v, int) and 0 <= v <= 0xffffffff for v in (op, seq, arg)):
        raise ValueError('invalid uint32 field')
    if len(payload) > MAX_PAYLOAD:
        raise ValueError('payload too large')
    if op == 1:
        if seq != 0 or arg not in (24000, 48000) or len(payload) != 8:
            raise ValueError('invalid START')
        if not 1 <= struct.unpack('<Q', payload)[0] <= arg * 120:
            raise ValueError('invalid sample count')
    elif op == 2:
        if arg or not payload or len(payload) % 2:
            raise ValueError('invalid PCM DATA')
    elif op in (3, 4):
        if arg or payload:
            raise ValueError('invalid END/STOP')
    else:
        raise ValueError('invalid operation')


def encode_frame(op, seq, arg=0, payload=b''):
    _validate(op, seq, arg, payload)
    prefix = struct.pack('<4sIIII', b'FMA1', op, seq, arg, len(payload))
    crc = zlib.crc32(payload, zlib.crc32(prefix))
    return prefix + struct.pack('<I', crc) + payload


def parse_frame(raw):
    if not 24 <= len(raw) <= 24 + MAX_PAYLOAD:
        raise ValueError('invalid frame length')
    magic, op, seq, arg, size, crc = HEADER.unpack_from(raw)
    if magic != b'FMA1' or size != len(raw) - 24:
        raise ValueError('invalid frame header')
    payload = raw[24:]
    if crc != zlib.crc32(payload, zlib.crc32(raw[:20])):
        raise ValueError('CRC mismatch')
    _validate(op, seq, arg, payload)
    return op, seq, arg, payload


def parse_response(line):
    if not line.startswith(b'FMA1 '):
        return None
    try:
        _, seq, kind, rate, completed = line.decode('ascii').strip().split()
        if not (seq.isdecimal() and rate.isdecimal() and completed.isdecimal()):
            raise ValueError('noninteger device event')
        seq, rate, completed = int(seq), int(rate), int(completed)
        if seq > 0xffffffff or rate not in (24000, 48000) or completed > 5760000:
            raise ValueError('out of bounds device event')
        if kind not in ('READY', 'ACK', 'DONE', 'STOPPED', 'ERROR'):
            raise ValueError('unknown device event')
        return seq, kind, rate, completed
    except (UnicodeDecodeError, ValueError) as exc:
        raise ValueError('malformed device response') from exc


def _event(transport, timeout):
    deadline = time.monotonic() + timeout
    pending = bytearray()
    while time.monotonic() < deadline:
        # Production pyserial read_until is bounded; test transports use readline.
        chunk = (transport.read_until(b'\n', 512) if hasattr(transport, 'read_until')
                 else transport.readline())
        if not chunk:
            continue
        pending.extend(chunk)
        if len(pending) > 4096:
            raise ValueError('oversized console response')
        if pending.endswith(b'\n'):
            result = parse_response(bytes(pending))
            pending.clear()
            if result is not None:
                return result
    raise TimeoutError('device response timeout; restoration unconfirmed')


def _validate_wav(path, rate):
    with wave.open(str(path), 'rb') as audio:
        if (audio.getnchannels(), audio.getsampwidth(), audio.getframerate(), audio.getcomptype()) != (1, 2, rate, 'NONE'):
            raise ValueError('requires PCM16 mono WAV with requested rate')
        count = audio.getnframes()
        if rate not in (24000, 48000) or not 1 <= count <= rate * 120:
            raise ValueError('invalid rate/duration')
        remaining = count
        while remaining:
            block = audio.readframes(min(1440, remaining))
            if not block or len(block) % 2:
                raise ValueError('truncated WAV')
            remaining -= len(block) // 2
        return count


def stream_audio(transport, path, rate, stop_requested=lambda: False, emit=lambda event: None, timeout=5):
    """Return (actual event, restored rate, physical count), never sent-byte proof.

    On mismatch/timeout stop transmitting. Firmware's idle deadline owns cleanup;
    no rollback success is inferred, and the exception remains visible.
    """
    count = _validate_wav(path, rate)
    seq, sent, completed = 0, 0, 0

    def exchange(op, arg=0, payload=b''):
        nonlocal seq, completed
        raw = encode_frame(op, seq, arg, payload)
        if transport.write(raw) != len(raw):
            raise ValueError('short serial write; restoration unconfirmed')
        response_seq, kind, actual_rate, physical = _event(transport, timeout)
        # A physical-button cancellation may arrive while any frame is waiting.
        expected = {1: 'READY', 2: 'ACK', 3: 'DONE', 4: 'STOPPED'}[op]
        if response_seq != seq or kind not in (expected, 'STOPPED'):
            raise ValueError(f'unexpected device event {response_seq} {kind}; restoration unconfirmed')
        if not completed <= physical <= sent:
            raise ValueError('invalid physical completion count')
        if kind in ('DONE', 'STOPPED'):
            if actual_rate != 24000 or (kind == 'DONE' and physical != count):
                raise ValueError('device completion/restoration mismatch')
        elif actual_rate != rate or (kind == 'READY' and physical != 0):
            raise ValueError('device profile/count mismatch')
        completed = physical
        seq += 1
        emit(f'FMA1 {response_seq} {kind} {actual_rate} {physical}')
        return kind, actual_rate, physical

    result = exchange(1, rate, struct.pack('<Q', count))
    if result[0] == 'STOPPED':
        return result
    with wave.open(str(path), 'rb') as audio:
        while sent < count:
            if stop_requested():
                return exchange(4)
            payload = audio.readframes(min(1440, count - sent))
            if not payload or len(payload) % 2:
                raise ValueError('WAV changed/truncated during streaming')
            sent += len(payload) // 2
            result = exchange(2, payload=payload)
            if result[0] == 'STOPPED':
                return result
    return exchange(4 if stop_requested() else 3)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--port', required=True, help='USB path or pyserial socket:// / rfc2217:// URL')
    parser.add_argument('--wav', type=Path, required=True)
    parser.add_argument('--rate', type=int, choices=(24000, 48000), required=True)
    args = parser.parse_args()
    import serial  # Test client dependency only; install on client, never firmware.
    cancelled = False
    def stop(_signum, _frame):
        nonlocal cancelled
        cancelled = True
    signal.signal(signal.SIGINT, stop)
    try:
        # RFC2217 uses its URL's network timeout; pyserial 3.5 does not
        # implement a serial write_timeout for this transport.
        transport = serial.serial_for_url(args.port, baudrate=115200, timeout=0.1,
                                          write_timeout=None if args.port.startswith('rfc2217://') else 5,
                                          do_not_open=True)
        # ESP32 USB bridges may wire modem-control lines to boot/reset.
        transport.dtr = False
        transport.rts = False
        transport.open()
        with transport:
            stream_audio(transport, args.wav, args.rate, stop_requested=lambda: cancelled,
                         emit=lambda event: print(event, flush=True))
    except (ValueError, TimeoutError, OSError, wave.Error, serial.SerialException) as exc:
        parser.exit(1, str(exc) + '\n')

if __name__ == '__main__':
    main()

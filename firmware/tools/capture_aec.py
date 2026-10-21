#!/usr/bin/env python3
"""Explicit, private, bounded UDP capture of StopWatch SWAC v1 diagnostics.

No socket or recording exists until capture() or the CLI is explicitly invoked.
Producer times are observations, not acoustic timestamps. Stream positions have
independent origins. No padding, alignment, DSP score or acoustic-pass inference.
"""
import argparse
import ipaddress
import json
import math
import os
from pathlib import Path
import socket
import signal
import threading
import struct
import sys
import time
import wave

# aec_diagnostic_wire.h WriteDiagnosticHeader: fixed endian offsets, not C layout.
HEADER = struct.Struct('<4sBBBBIIQQIHHII')
MAX_DATAGRAM = 1200
MAX_BYTES = 32 * 1024 * 1024
SUMMARY_RESERVE = 8192


def parse_packet(data):
    """Validate every bound before any file write; return header metadata + PCM."""
    if not HEADER.size <= len(data) <= MAX_DATAGRAM:
        raise ValueError('datagram bounds')
    (magic, version, stream, channels, flags, sequence, generation, position,
     timestamp, rate, frames, payload_bytes, drops, window) = HEADER.unpack_from(data)
    if (magic != b'SWAC' or version != 1 or stream not in (1, 2)
            or channels != (2 if stream == 1 else 1) or flags & ~3
            or rate != 16000 or frames == 0
            or payload_bytes != frames * channels * 2
            or len(data) != HEADER.size + payload_bytes):
        raise ValueError('invalid SWAC v1 packet')
    return dict(stream=stream, channels=channels, flags=flags, sequence=sequence,
                generation=generation, position=position, timestamp_us=timestamp,
                rate=rate, frames=frames, payload_bytes=payload_bytes, drops=drops,
                window=window, reference_valid=bool(flags & 1)), data[HEADER.size:]


def private_file(path):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    os.fchmod(fd, 0o600)
    return os.fdopen(fd, 'wb')


def json_bytes(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':')) + '\n').encode('utf-8')


def discontinuities(previous, current, after_unordered, high_sequence):
    if previous is None:
        return ['first'], False
    reasons = []
    for key, label in [('window', 'window'), ('generation', 'generation'),
                       ('reference_valid', 'reference_validity')]:
        if current[key] != previous[key]:
            reasons.append(label)
    unordered = False
    if current['window'] == previous['window']:
        if current['sequence'] <= high_sequence:
            reasons.append('duplicate_or_out_of_order')
            unordered = True
        elif current['sequence'] != previous['sequence'] + 1:
            reasons.append('sequence_gap')
    if current['flags'] & 2:
        reasons.append('reset')
    if current['position'] != previous['position'] + previous['frames']:
        reasons.append('position')
    if current['timestamp_us'] < previous['timestamp_us']:
        reasons.append('timestamp_regression')
    if after_unordered:
        reasons.append('after_unordered')
    return reasons, unordered


def capture(bind, port, duration, output, *, max_bytes=MAX_BYTES, source=None,
            stop_event=None, on_ready=None):
    """Capture one bounded session. on_ready supports loopback-only host fixtures.

    The first valid source endpoint is pinned; an optional source IPv4 narrows it
    further. A new directory is mandatory. At most two WAV handles remain open.
    Disk budget counts packet framing, index, WAV headers/PCM and final summary;
    the reserved summary space is never spent on packets. Memory stays constant.
    """
    bind = str(ipaddress.IPv4Address(bind))
    if source is not None:
        source = str(ipaddress.IPv4Address(source))
    if (not math.isfinite(duration) or not 0 < duration <= 120
            or not isinstance(port, int) or not 0 <= port <= 65535
            or not isinstance(max_bytes, int) or not SUMMARY_RESERVE <= max_bytes <= MAX_BYTES):
        raise ValueError('invalid duration, port or total byte budget')
    output = Path(output)
    output.mkdir(mode=0o700)  # deliberately refuses existing directory/symlink
    output.chmod(0o700)
    summary = dict(schema_version=1, wire='SWAC-v1', stop_reason='error',
                   rejected_packets=0, filtered_packets=0, accepted_packets=0,
                   bytes_written=0, streams={},
                   timing='independent stream origins; producer times are not acoustic times')
    states = {}
    written = 0
    endpoint = None
    packets_file = index_file = None
    try:
        packets_file = private_file(output/'packets.bin')
        index_file = private_file(output/'index.jsonl')
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as receiver:
            receiver.bind((bind, port))
            deadline = time.monotonic() + duration
            if on_ready:
                on_ready(receiver.getsockname())
            while True:
                remaining = deadline - time.monotonic()
                if stop_event is not None and stop_event.is_set():
                    summary['stop_reason'] = 'stop'
                    break
                if remaining <= 0:
                    summary['stop_reason'] = 'timeout'
                    break
                receiver.settimeout(min(.05, remaining))
                try:
                    # The extra byte detects even truncated oversized datagrams.
                    data, sender = receiver.recvfrom(MAX_DATAGRAM + 1)
                except socket.timeout:
                    continue
                if (source is not None and sender[0] != source) or (endpoint and sender != endpoint):
                    summary['filtered_packets'] += 1
                    continue
                try:
                    packet, pcm = parse_packet(data)
                except ValueError:
                    summary['rejected_packets'] += 1
                    continue
                key = str(packet['stream'])
                state = states.get(key)
                previous = state['previous'] if state else None
                reasons, unordered = discontinuities(previous, packet,
                                                      state['unordered'] if state else False,
                                                      state['high_sequence'] if state else None)
                segment_number = (state['segment_number'] + bool(reasons)) if state else 1
                segment = f'stream-{key}-{segment_number:06d}.wav'
                record = dict(packet, packet_offset=packets_file.tell(),
                              packet_length=len(data), segment=segment,
                              reasons=reasons, source_ipv4=sender[0], source_port=sender[1])
                row = json_bytes(record)
                cost = 4 + len(data) + len(row) + len(pcm) + (44 if reasons else 0)
                if written + cost + SUMMARY_RESERVE > max_bytes:
                    summary['stop_reason'] = 'budget'
                    break
                endpoint = endpoint or sender
                if state is None:
                    totals = dict(packets=0, frames=0, samples=0, missing_packets=0,
                                  unordered_packets=0, generation_changes=0,
                                  first_generation=packet['generation'],
                                  last_generation=packet['generation'], segments=0,
                                  invalid_reference_frames=0)
                    summary['streams'][key] = totals
                    state = dict(segment_number=0, writer=None, handle=None, high_sequence=None)
                    states[key] = state
                totals = summary['streams'][key]
                if reasons:
                    if state['writer']:
                        state['writer'].close()
                        state['handle'].close()
                    handle = private_file(output/segment)
                    writer = wave.open(handle, 'wb')
                    writer.setnchannels(packet['channels'])
                    writer.setsampwidth(2)
                    writer.setframerate(16000)
                    state.update(writer=writer, handle=handle, segment_number=segment_number)
                    totals['segments'] += 1
                # High-water sequence avoids counting a replay as newly missing
                # data. Every anomaly still forces a separate WAV segment.
                if previous is None or packet['window'] != previous['window']:
                    state['high_sequence'] = packet['sequence']
                elif packet['sequence'] > state['high_sequence']:
                    totals['missing_packets'] += packet['sequence'] - state['high_sequence'] - 1
                    state['high_sequence'] = packet['sequence']
                if previous and packet['generation'] != previous['generation']:
                    totals['generation_changes'] += 1
                totals['unordered_packets'] += int(unordered)
                totals['last_generation'] = packet['generation']
                totals['packets'] += 1
                totals['frames'] += packet['frames']
                totals['samples'] += packet['frames'] * packet['channels']
                if not packet['reference_valid']:
                    totals['invalid_reference_frames'] += packet['frames']
                packets_file.write(struct.pack('<I', len(data)) + data)
                index_file.write(row)
                state['writer'].writeframesraw(pcm)
                state.update(previous=packet, unordered=unordered)
                summary['accepted_packets'] += 1
                written += cost
    except KeyboardInterrupt:
        summary['stop_reason'] = 'interrupt'
    finally:
        for state in states.values():
            if state['writer']:
                state['writer'].close()
                state['handle'].close()
        for handle in (packets_file, index_file):
            if handle:
                handle.close()
        # Include the summary itself, including the decimal digit count of its size.
        with os.scandir(output) as files:
            written = sum(entry.stat().st_size for entry in files)
        summary['bytes_written'] = written
        while True:
            data = json_bytes(summary)
            total = written + len(data)
            if total == summary['bytes_written']:
                break
            summary['bytes_written'] = total
        if len(data) > SUMMARY_RESERVE:
            raise RuntimeError('summary reserve exceeded')
        with private_file(output/'summary.json') as handle:
            handle.write(data)
    return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--bind', required=True, help='local numeric IPv4 address')
    parser.add_argument('--port', required=True, type=int, help='UDP port 1..65535')
    parser.add_argument('--duration', required=True, type=float, help='seconds, >0 and <=120')
    parser.add_argument('--output', required=True, type=Path, help='new private directory, outside Git')
    parser.add_argument('--source', help='optional exact sender IPv4 filter')
    parser.add_argument('--max-bytes', type=int, default=MAX_BYTES, help='total saved bytes, <=33554432')
    args = parser.parse_args()
    if not 1 <= args.port <= 65535:
        parser.error('--port must be 1..65535')
    # Finish the current packet atomically with respect to a terminal Ctrl-C.
    stop = threading.Event()
    old_handler = signal.signal(signal.SIGINT, lambda signum, frame: stop.set())
    try:
        result = capture(args.bind, args.port, args.duration, args.output,
                         max_bytes=args.max_bytes, source=args.source, stop_event=stop)
    except (OSError, ValueError):
        print('capture-aec: failed; check explicit limits, bind and new private output path', file=sys.stderr)
        return 1
    finally:
        signal.signal(signal.SIGINT, old_handler)
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == '__main__':
    sys.exit(main())

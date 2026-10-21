"""Real loopback-only SWAC fixtures; no device, SDK, network host or raw audio."""
import importlib.util
import json
import os
from pathlib import Path
import socket
import signal
import subprocess
import sys
import struct
import tempfile
import threading
import time
import unittest
import wave

SCRIPT = Path(__file__).resolve().parents[1] / 'tools/capture_aec.py'
HEADER = struct.Struct('<4sBBBBIIQQIHHII')


def packet(stream=1, sequence=0, generation=7, position=0, timestamp=1000,
           flags=1, window=3, pcm=None):
    channels = 2 if stream == 1 else 1
    pcm = pcm if pcm is not None else struct.pack('<' + 'h' * channels * 3,
                                                *range(channels * 3))
    return HEADER.pack(b'SWAC', 1, stream, channels, flags, sequence, generation,
                       position, timestamp, 16000, len(pcm)//(channels*2), len(pcm),
                       0, window) + pcm


class CaptureTests(unittest.TestCase):
    def setUp(self):
        self.assertTrue(SCRIPT.is_file(), 'explicit bounded collector is not implemented')
        spec = importlib.util.spec_from_file_location('capture_aec', SCRIPT)
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.output = Path(self.temp.name) / 'private'

    def capture(self, packets=(), duration=.12, stop=None, budget=32768, source=None):
        errors = []
        def ready(address):
            def send():
                try:
                    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sender:
                        for item in packets:
                            sender.sendto(item, address)
                    if stop:
                        time.sleep(.03)
                        stop.set()
                except BaseException as exc:
                    errors.append(exc)
            self.sender = threading.Thread(target=send)
            self.sender.start()
        result = self.module.capture('127.0.0.1', 0, duration, self.output,
                                     max_bytes=budget, source=source,
                                     stop_event=stop, on_ready=ready)
        self.sender.join(timeout=1)
        self.assertFalse(errors)
        return result

    def rows(self):
        return [json.loads(line) for line in (self.output/'index.jsonl').read_text().splitlines()]

    def samples(self, name):
        with wave.open(str(self.output/name), 'rb') as saved:
            self.assertEqual(saved.getframerate(), 16000)
            self.assertEqual(saved.getsampwidth(), 2)
            return saved.readframes(saved.getnframes())

    def test_loopback_saves_exact_packets_and_pcm_with_independent_origins(self):
        raw1 = packet(flags=3)
        afe = packet(2, position=99, timestamp=1200, flags=3)
        raw2 = packet(sequence=1, position=3, timestamp=1300)
        result = self.capture([raw1, afe, raw2])
        rows = self.rows()
        self.assertEqual(len(rows), 3)
        self.assertEqual(self.samples(rows[0]['segment']), raw1[48:] + raw2[48:])
        self.assertEqual(self.samples(rows[1]['segment']), afe[48:])
        self.assertEqual(result['streams']['1']['frames'], 6)
        self.assertEqual(result['streams']['2']['frames'], 3)
        saved = (self.output/'packets.bin').read_bytes()
        self.assertEqual(saved, b''.join(struct.pack('<I', len(p)) + p for p in [raw1, afe, raw2]))
        self.assertEqual(json.loads((self.output/'summary.json').read_text()), result)
        self.assertEqual(os.stat(self.output).st_mode & 0o777, 0o700)
        for child in self.output.iterdir():
            self.assertEqual(child.stat().st_mode & 0o777, 0o600)
        self.assertEqual(result['stop_reason'], 'timeout')

    def test_gaps_duplicate_out_of_order_and_validity_never_join_continuity(self):
        items = [packet(sequence=0), packet(sequence=2, position=6),
                 packet(sequence=2, position=6), packet(sequence=1, position=3),
                 packet(sequence=3, position=9), packet(sequence=4, position=12, flags=0)]
        result = self.capture(items)
        rows = self.rows()
        self.assertEqual(len({r['segment'] for r in rows}), 6)
        self.assertIn('sequence_gap', rows[1]['reasons'])
        self.assertIn('duplicate_or_out_of_order', rows[2]['reasons'])
        self.assertIn('duplicate_or_out_of_order', rows[3]['reasons'])
        self.assertIn('after_unordered', rows[4]['reasons'])
        self.assertFalse(rows[-1]['reference_valid'])
        self.assertIn('reference_validity', rows[-1]['reasons'])
        self.assertEqual(result['streams']['1']['missing_packets'], 1)
        self.assertEqual(result['streams']['1']['unordered_packets'], 2)
        self.assertEqual([self.samples(r['segment']) for r in rows], [p[48:] for p in items])

    def test_replayed_run_never_becomes_a_continuous_segment(self):
        items = [packet(sequence=5, position=15), packet(sequence=1, position=3),
                 packet(sequence=2, position=6), packet(sequence=3, position=9),
                 packet(sequence=6, position=18)]
        result = self.capture(items)
        rows = self.rows()
        self.assertEqual(len({r['segment'] for r in rows}), len(items))
        self.assertEqual(result['streams']['1']['unordered_packets'], 3)
        for row in rows[1:4]:
            self.assertIn('duplicate_or_out_of_order', row['reasons'])

    def test_reset_generation_window_position_and_time_changes_split(self):
        items = [packet(flags=3), packet(sequence=1, flags=3, timestamp=1100),
                 packet(sequence=2, generation=8, timestamp=1200),
                 packet(sequence=0, generation=8, window=4, timestamp=1300),
                 packet(sequence=1, generation=8, window=4, position=9, timestamp=1400),
                 packet(sequence=2, generation=8, window=4, position=12, timestamp=100)]
        result = self.capture(items)
        rows = self.rows()
        self.assertEqual(len({r['segment'] for r in rows}), len(items))
        for index, reason in [(1, 'reset'), (2, 'generation'), (3, 'window'),
                              (4, 'position'), (5, 'timestamp_regression')]:
            self.assertIn(reason, rows[index]['reasons'])
        self.assertEqual(result['streams']['1']['generation_changes'], 1)
        self.assertEqual([r['generation'] for r in rows], [7, 7, 8, 8, 8, 8])

    def test_rejects_malformed_oversize_truncated_and_incomplete_frames(self):
        valid = packet()
        bad = [b'', valid[:47], b'NOPE' + valid[4:], valid[:4]+b'\x02'+valid[5:],
               valid[:7]+b'\x04'+valid[8:], valid[:6]+b'\x01'+valid[7:],
               valid[:-1], valid+b'\x00', packet(pcm=b''), b'x'*1201]
        for offset, fmt, value in [(5, 'B', 3), (32, 'I', 24000), (36, 'H', 4), (38, 'H', 11)]:
            changed = bytearray(valid)
            struct.pack_into('<'+fmt, changed, offset, value)
            bad.append(bytes(changed))
        result = self.capture(bad + [valid])
        self.assertEqual(result['rejected_packets'], len(bad))
        self.assertEqual(len(self.rows()), 1)
        self.assertEqual(self.samples(self.rows()[0]['segment']), valid[48:])

    def test_total_disk_budget_includes_index_wave_and_summary(self):
        result = self.capture([packet(sequence=i, position=i*3, flags=3) for i in range(300)],
                              budget=12000)
        self.assertEqual(result['stop_reason'], 'budget')
        self.assertLessEqual(sum(p.stat().st_size for p in self.output.iterdir()), 12000)
        self.assertLessEqual(result['bytes_written'], 12000)
        self.assertGreater(len(self.rows()), 0)

    def test_explicit_stop_closes_wave_without_waiting_for_duration(self):
        start = time.monotonic()
        result = self.capture([packet()], duration=2, stop=threading.Event())
        self.assertEqual(result['stop_reason'], 'stop')
        self.assertLess(time.monotonic()-start, .6)
        self.assertEqual(self.samples(self.rows()[0]['segment']), packet()[48:])

    def test_timeout_and_source_filter_save_no_unexpected_audio(self):
        result = self.capture([packet()], source='127.0.0.2')
        self.assertEqual(result['stop_reason'], 'timeout')
        self.assertEqual(result['filtered_packets'], 1)
        self.assertEqual(self.rows(), [])
        self.assertEqual((self.output/'packets.bin').read_bytes(), b'')

    def test_cli_ctrl_c_finishes_private_files_and_summary(self):
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as reservation:
            reservation.bind(('127.0.0.1', 0))
            port = reservation.getsockname()[1]
        process = subprocess.Popen([sys.executable, str(SCRIPT), '--bind', '127.0.0.1',
                                    '--port', str(port), '--duration', '60',
                                    '--output', str(self.output)],
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.addCleanup(lambda: process.kill() if process.poll() is None else None)
        deadline = time.monotonic() + 2
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sender:
            while time.monotonic() < deadline:
                sender.sendto(packet(), ('127.0.0.1', port))
                time.sleep(.02)
                waves = list(self.output.glob('*.wav'))
                if waves and waves[0].stat().st_size > 44:
                    break
            else:
                self.fail('CLI did not receive synthetic loopback fixture')
        process.send_signal(signal.SIGINT)
        stdout, stderr = process.communicate(timeout=2)
        self.assertEqual(process.returncode, 0, stderr)
        result = json.loads(stdout)
        self.assertEqual(result['stop_reason'], 'stop')
        self.assertTrue(self.rows())
        self.assertEqual(self.samples(self.rows()[0]['segment']), packet()[48:])
        self.assertEqual(result['bytes_written'], sum(p.stat().st_size for p in self.output.iterdir()))

    def test_first_valid_source_endpoint_is_pinned(self):
        def ready(address):
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as first:
                first.sendto(packet(), address)
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as second:
                second.sendto(packet(sequence=1, position=3), address)
        result = self.module.capture('127.0.0.1', 0, .1, self.output, on_ready=ready)
        self.assertEqual(result['accepted_packets'], 1)
        self.assertEqual(result['filtered_packets'], 1)

    def test_invalid_limits_address_existing_output_refused_before_binding(self):
        for kwargs in [dict(duration=0), dict(duration=121), dict(duration=float('nan')),
                       dict(max_bytes=32*1024*1024+1), dict(max_bytes=8191),
                       dict(bind='localhost'), dict(source='::1'), dict(port=65536)]:
            options = dict(bind='127.0.0.1', port=0, duration=.1, output=self.output)
            options.update(kwargs)
            with self.subTest(kwargs=kwargs), self.assertRaises(ValueError):
                self.module.capture(**options)
            self.assertFalse(self.output.exists())
        self.output.mkdir()
        with self.assertRaises(FileExistsError):
            self.module.capture('127.0.0.1', 0, .1, self.output)


if __name__ == '__main__':
    unittest.main()

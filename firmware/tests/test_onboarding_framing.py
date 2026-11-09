import os
from pathlib import Path
import subprocess
import tempfile
import unittest


class OnboardingFramingTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.directory = tempfile.TemporaryDirectory()
        cls.binary = Path(cls.directory.name) / 'framing-test'
        source = Path(__file__).with_name('onboarding_framing_host.c')
        subprocess.run([os.environ.get('CC', 'cc'), '-std=c11', '-Wall', '-Wextra',
                        '-Werror', '-o', str(cls.binary), str(source)], check=True)

    @classmethod
    def tearDownClass(cls):
        cls.directory.cleanup()

    def run_case(self, name):
        completed = subprocess.run([str(self.binary), name], capture_output=True, text=True)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(completed.stdout.strip(), 'ok')

    def test_large_official_payload_is_unchanged_over_20_byte_frames(self):
        self.run_case('roundtrip')

    def test_bad_lengths_offsets_identity_and_expiry_are_rejected(self):
        self.run_case('rejects')

    def test_raw_client_mode_empty_response_and_no_downgrade(self):
        self.run_case('raw')

    def test_user_input_pause_between_complete_exchanges(self):
        self.run_case('pause')

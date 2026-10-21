from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class DuplexReferenceTest(unittest.TestCase):
    def test_cpp_behavior(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / "reference-test"
            compile_result = subprocess.run([
                "c++", "-std=c++17", "-Wall", "-Wextra", "-Werror", "-pedantic",
                "-I", str(ROOT / "firmware/overlays/main/boards/m5stack/stopwatch"),
                str(ROOT / "firmware/tests/duplex_reference_test.cc"),
                "-o", str(binary),
            ], capture_output=True, text=True)
            self.assertEqual(compile_result.returncode, 0, compile_result.stderr)
            result = subprocess.run([str(binary)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)

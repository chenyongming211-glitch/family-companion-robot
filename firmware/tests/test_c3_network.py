"""Cloud host tests against the exact registry component via C3_NETWORK_COMPONENT.

No component downloads or runtime tests belong on the Mac. Set the environment
variable to an extracted verified 78/esp-ml307 3.7.6 on the cloud test machine.
"""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[2]
SCRIPT = REPO / "firmware/overlays/scripts/family_c3_network.py"
AUDIO = REPO / "firmware/overlays/main/audio"
TESTS = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("family_c3_network", SCRIPT)
network = importlib.util.module_from_spec(spec)
spec.loader.exec_module(network)


def tree_hashes(root):
    return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in root.rglob("*") if p.is_file()}


class GeneratorAnchors(unittest.TestCase):
    def test_anchor_fail_closed(self):
        for source in ("missing", "anchor anchor"):
            with self.assertRaisesRegex(ValueError, "not unique"):
                network.replace_once(source, "anchor", "replacement")
        with self.assertRaisesRegex(ValueError, "not unique"):
            network.replace_function("first first next", "first", "next", "x")


@unittest.skipUnless(os.environ.get("C3_NETWORK_COMPONENT"),
                     "cloud-only exact component fixture not configured")
class C3Network(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.component = Path(os.environ["C3_NETWORK_COMPONENT"]).resolve()
        cls.before = tree_hashes(cls.component)
        cls.temp = tempfile.TemporaryDirectory(prefix="c3-network-")
        cls.work = Path(cls.temp.name)
        cls.generated = cls.work / "generated"
        cls.manifest = network.generate(cls.component, cls.generated)

    @classmethod
    def tearDownClass(cls):
        try:
            if tree_hashes(cls.component) != cls.before:
                raise AssertionError("managed component changed")
        finally:
            cls.temp.cleanup()

    def test_manifest_repeatable_and_managed_immutable(self):
        self.assertEqual(self.before, tree_hashes(self.component))
        first = tree_hashes(self.generated)
        network.generate(self.component, self.generated)
        self.assertEqual(first, tree_hashes(self.generated))
        manifest = json.loads((self.generated / "family_c3_network.manifest.json").read_text())
        self.assertEqual(manifest, self.manifest)
        self.assertEqual(manifest["original_sha256"], network.ORIGINAL_SHA256)
        for name, digest in manifest["patched_sha256"].items():
            self.assertEqual(digest, hashlib.sha256((self.generated / name).read_bytes()).hexdigest())
        for name, digest in manifest["helper_sha256"].items():
            self.assertEqual(digest, hashlib.sha256((AUDIO / name).read_bytes()).hexdigest())
        self.assertEqual((self.generated / "LICENSE.esp-ml307").read_bytes(),
                         (self.component / "LICENSE").read_bytes())
        self.assertEqual(self.before, tree_hashes(self.component))

    def test_every_original_pin_rejects_drift_before_output(self):
        fixture = self.work / "mutated"
        fixture.mkdir(exist_ok=True)
        for name in network.ORIGINAL_SHA256:
            dest = fixture / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(self.component / name, dest)
        for index, name in enumerate(network.ORIGINAL_SHA256):
            with self.subTest(name=name):
                dest = fixture / name
                original = dest.read_bytes()
                dest.write_bytes(original + b"\n")
                out = self.work / f"must-not-exist-{index}"
                with self.assertRaisesRegex(ValueError, "SHA256 mismatch"):
                    network.generate(fixture, out)
                self.assertFalse(out.exists())
                dest.write_bytes(original)

    def test_refuses_managed_output_and_symlink(self):
        with self.assertRaisesRegex(ValueError, "immutable component"):
            network.generate(self.component, self.component / "build")
        out = self.work / "symlink-output"
        out.mkdir(exist_ok=True)
        target = self.work / "do-not-write"
        target.write_text("unchanged")
        (out / "web_socket.cc").symlink_to(target)
        with self.assertRaisesRegex(ValueError, "symlink refused"):
            network.generate(self.component, out)
        self.assertEqual(target.read_text(), "unchanged")
        self.assertFalse((out / "esp_tcp.cc").exists())

    def build_run(self, name, sources, extra=(), standard="c++23"):
        for sanitized in (False, True):
            with self.subTest(name=name, sanitized=sanitized):
                output = self.work / f"{name}-{'asan' if sanitized else 'O2'}"
                flags = (["-O1", "-g", "-fsanitize=address,undefined", "-fno-omit-frame-pointer", "-no-pie"]
                         if sanitized else ["-O2"])
                command = [os.environ.get("CXX", "g++"), f"-std={standard}", *flags,
                           "-Wall", "-Wextra", "-Werror", "-pthread",
                           "-I", str(TESTS / "c3_network_stubs"),
                           "-I", str(self.component),
                           "-I", str(self.component / "include"),
                           "-I", str(self.component / "src/esp"), "-I", str(AUDIO),
                           *(str(source) for source in sources), *extra, "-o", str(output)]
                subprocess.run(command, check=True)
                environment = dict(os.environ, ASAN_OPTIONS="detect_leaks=1:halt_on_error=1",
                                   UBSAN_OPTIONS="halt_on_error=1:print_stacktrace=1")
                if sanitized:
                    # The cloud image has /etc/ld.so.preload instrumentation.
                    # Load ASan first for this child only; do not change the host.
                    asan = subprocess.check_output(
                        [command[0], "-print-file-name=libasan.so"], text=True).strip()
                    environment["LD_PRELOAD"] = asan + (
                        ":" + environment["LD_PRELOAD"] if environment.get("LD_PRELOAD") else "")
                subprocess.run([str(output)], check=True, env=environment)

    def test_actual_generated_websocket_public_callbacks(self):
        self.build_run("websocket", [TESTS / "c3_network_websocket_host.cc", self.generated / "web_socket.cc"],
                       ["-Wno-misleading-indentation"])

    def test_actual_generated_tcp_syscall_contracts(self):
        self.build_run("tcp", [TESTS / "c3_network_tcp_host.cc", self.generated / "esp_tcp.cc"],
                       ["-Wl,--wrap=send,--wrap=connect,--wrap=select,--wrap=clock_gettime"])

    def test_helpers_and_posix_blocked_peer(self):
        self.build_run("helpers", [TESTS / "c3_network_helpers_host.cc"], standard="c++17")


if __name__ == "__main__":
    unittest.main()

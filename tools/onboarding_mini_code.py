#!/usr/bin/env python3
"""Generate a private device mini-program code on the operator's cloud server.

Only the official WeChat HTTPS endpoints are reachable through this CLI. Test
transport injection is a Python function parameter, never an operator option.
"""
import argparse
import http.client
import json
import os
import re
import ssl
import stat
import sys
from urllib.parse import urlencode

MAX_INPUT = 16 * 1024
MAX_IMAGE = 1024 * 1024
ERROR = "Mini-program code generation failed."
BUNDLE_FIELDS = {
    "schema_version", "device_id", "mac", "serial", "model", "device_key",
    "claim_code", "bootstrap_url", "voice_url",
}


class MiniCodeError(Exception):
    """Failure message never includes file contents or upstream diagnostics."""


def _fail():
    raise MiniCodeError(ERROR)


def _pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            _fail()
        result[key] = value
    return result


def _json(data):
    value = json.loads(data.decode("utf-8"), object_pairs_hook=_pairs,
                       parse_constant=lambda _: _fail())
    if not isinstance(value, dict):
        _fail()
    return value


def _parent(path):
    """Pin each directory without following symlinks; retain the final dirfd."""
    path = os.fspath(path)
    if not isinstance(path, str) or not path or len(path) > 4096:
        _fail()
    parts = path.split("/")
    if not parts[-1] or any(part in (".", "..") for part in parts):
        _fail()
    fd = os.open("/" if path.startswith("/") else ".", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        for part in parts[:-1]:
            if not part:
                continue
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
            os.close(fd)
            fd = child
        return fd, parts[-1]
    except BaseException:
        os.close(fd)
        raise


def _private_json(path):
    directory, name = _parent(path)
    try:
        # O_NONBLOCK prevents a FIFO/device supplied in place of a regular file
        # from blocking before fstat. Read and validate the same open inode.
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
        try:
            info = os.fstat(fd)
            if (not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600
                    or info.st_uid != os.geteuid() or info.st_nlink != 1
                    or info.st_size > MAX_INPUT):
                _fail()
            data = bytearray()
            while len(data) <= MAX_INPUT:
                block = os.read(fd, MAX_INPUT + 1 - len(data))
                if not block:
                    break
                data.extend(block)
            if len(data) > MAX_INPUT:
                _fail()
            return _json(data)
        finally:
            os.close(fd)
    finally:
        os.close(directory)


def _matches(pattern, value):
    return isinstance(value, str) and re.fullmatch(pattern, value) is not None


def _inputs(bundle_path, config_path):
    bundle, config = _private_json(bundle_path), _private_json(config_path)
    if (set(bundle) != BUNDLE_FIELDS or type(bundle["schema_version"]) is not int
            or bundle["schema_version"] != 1
            or not _matches(r"[0-9a-f]{32}", bundle["claim_code"])
            or not _matches(r"dev_[0-9a-f]{32}", bundle["device_id"])
            or not _matches(r"(?:[0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}", bundle["mac"])
            or not _matches(r"[A-Za-z0-9_-]{1,64}", bundle["serial"])
            or bundle["model"] not in ("微雪 ESP32-S3", "M5 ESP32-S3")
            or bundle["bootstrap_url"] != "https://robot.songguoxue.com/api/device/bootstrap"
            or bundle["voice_url"] != "wss://robot.songguoxue.com/xiaozhi/v1/"
            or set(config) != {"appid", "secret"}
            or not _matches(r"wx[0-9a-f]{16}", config["appid"])
            or not _matches(r"[0-9A-Fa-f]{32}", config["secret"])):
        _fail()
    # device_key is deliberately not accessed or sent. It belongs to device
    # authentication, not to WeChat code generation.
    return bundle["claim_code"], config


def _post(path, payload, maximum, factory):
    connection = factory("api.weixin.qq.com", timeout=10, context=ssl.create_default_context())
    try:
        connection.request("POST", path, body=json.dumps(payload, separators=(",", ":")).encode("utf-8"),
                           headers={"Content-Type": "application/json", "Accept": "application/json, image/png, image/jpeg"})
        response = connection.getresponse()
        # http.client never follows redirects, and never uses proxy env vars.
        if response.status != 200:
            _fail()
        content_type = response.getheader("Content-Type", "").split(";", 1)[0].strip().lower()
        data = response.read(maximum + 1)
        if len(data) > maximum:
            _fail()
        return content_type, data
    finally:
        connection.close()


def _image(data, content_type):
    # Check real file magic and terminal marker as well as the HTTP type.
    if content_type == "image/png":
        return (data.startswith(b"\x89PNG\r\n\x1a\n") and len(data) >= 45
                and data[12:16] == b"IHDR" and data.endswith(b"\x00\x00\x00\x00IEND\xaeB`\x82"))
    if content_type == "image/jpeg":
        return len(data) >= 4 and data.startswith(b"\xff\xd8\xff") and data.endswith(b"\xff\xd9")
    return False


def generate(bundle_path, config_path, environment, output_path, *, connection_factory=None):
    """Write one new 0600 image or raise only a sanitized MiniCodeError.

    All input and output checks occur before sending secrets. The output parent
    must be owned by this uid and 0700. No token cache or diagnostic log is made.
    """
    directory = None
    fd = None
    created = False
    try:
        if environment not in ("develop", "trial", "release"):
            _fail()
        scene, config = _inputs(bundle_path, config_path)
        directory, name = _parent(output_path)
        info = os.fstat(directory)
        if info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o700:
            _fail()
        try:
            os.stat(name, dir_fd=directory, follow_symlinks=False)
        except FileNotFoundError:
            pass
        else:
            _fail()
        factory = connection_factory or http.client.HTTPSConnection
        token_type, token_data = _post("/cgi-bin/stable_token", {
            "grant_type": "client_credential", "appid": config["appid"],
            "secret": config["secret"], "force_refresh": False,
        }, MAX_INPUT, factory)
        if token_type != "application/json":
            _fail()
        token_result = _json(token_data)
        token = token_result.get("access_token")
        if (token_result.get("errcode", 0) != 0
                or not _matches(r"[A-Za-z0-9_.-]{1,512}", token)
                or type(token_result.get("expires_in")) is not int
                or not 0 < token_result["expires_in"] <= 7200):
            _fail()
        image_type, image_data = _post("/wxa/getwxacodeunlimit?" + urlencode({"access_token": token}), {
            "scene": scene, "page": "pages/index/index", "env_version": environment,
            "check_path": environment == "release",
        }, MAX_IMAGE, factory)
        if not _image(image_data, image_type):
            _fail()
        fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
        created = True
        os.fchmod(fd, 0o600)
        pending = memoryview(image_data)
        while pending:
            written = os.write(fd, pending)
            if written <= 0:
                _fail()
            pending = pending[written:]
        os.fsync(fd)
        os.close(fd)
        fd = None
        os.fsync(directory)
    except Exception:
        if fd is not None:
            os.close(fd)
            fd = None
        if created:
            try:
                os.unlink(name, dir_fd=directory)
                os.fsync(directory)
            except OSError:
                pass
        # Upstream error bodies, URLs and file contents are never re-emitted.
        raise MiniCodeError(ERROR) from None
    finally:
        if fd is not None:
            os.close(fd)
        if directory is not None:
            os.close(directory)


class _Parser(argparse.ArgumentParser):
    def error(self, message):
        _fail()


def main(argv=None):
    try:
        parser = _Parser(description=__doc__)
        parser.add_argument("--bundle", required=True)
        parser.add_argument("--wechat-config", required=True)
        parser.add_argument("--environment", required=True, choices=("develop", "trial", "release"))
        parser.add_argument("--output", required=True)
        args = parser.parse_args(argv)
        generate(args.bundle, args.wechat_config, args.environment, args.output)
    except Exception:
        print(ERROR, file=sys.stderr)
        return 1
    # Do not echo the operator's path: it could itself contain private material.
    print("Created private mini-program code image.")
    return 0


if __name__ == "__main__":
    sys.exit(main())

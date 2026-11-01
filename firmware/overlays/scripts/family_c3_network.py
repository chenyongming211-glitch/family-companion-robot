#!/usr/bin/env python3
"""Generate C3-only bounded build inputs from the byte-pinned esp-ml307 3.7.6.

Never edits managed sources or dependency locks. CMake must replace only the two
SOURCES on the existing component target under CONFIG_XMINI_C3_PCM and add the
helper include directory. Original component headers/ABI and license stay intact.
"""
import argparse
import hashlib
import json
from pathlib import Path

SOURCE_URI = "https://components-file.espressif.com/components/78/esp-ml307/3.7.6/78__esp-ml307-v3.7.6.zip"
SOURCE_COMMIT = "bf62ab3daf30965c18cde113e6c93df3cada6363"
ARCHIVE_SHA256 = "b7d30d48a8cd8fa98b061673afdb2abdab10ad994ab71d7d27edbc0ada956036"
COMPONENT_HASH = "cc43276d047bfece6142bb554c0264520f27fc8b4a5e2a23796099d485a99541"
ORIGINAL_SHA256 = {
    "src/web_socket.cc": "978edda0e10368e4effdc17e96d57bffc67b47c98732da5bfaf5295c656b722e",
    "src/esp/esp_tcp.cc": "754535c5c9ed9127984cd6279993896e09761ae5995d2b7cebdc4fd7777f2752",
    "include/web_socket.h": "07880946174351e967191f90b43a59964272aa064132cdfffda7e457205764a1",
    "include/tcp.h": "d81aee7b684debcce73211bf9dc90284301c2aaa3eaf6b676c534c6523847694",
    "src/esp/esp_tcp.h": "f64df70b228cb566ae17c6f80aabd9c1ffc4de2b1bba30caa9e8b24d6a9d3292",
    "LICENSE": "c71d239df91726fc519c6eb72d318ec65820627232b2f796219e87dcf35d0ab4",
}
HELPERS = ("c3_websocket_bounds.h", "c3_tcp_deadline.h")


def sha(data):
    return hashlib.sha256(data).hexdigest()


def replace_once(source, old, new):
    if source.count(old) != 1:
        raise ValueError(f"source anchor is not unique: {old[:70]!r}")
    return source.replace(old, new, 1)


def replace_function(source, start, next_start, replacement):
    if source.count(start) != 1 or source.count(next_start) != 1:
        raise ValueError(f"function anchor is not unique: {start}")
    begin = source.index(start)
    end = source.index(next_start, begin)
    return source[:begin] + replacement.strip() + "\n\n" + source[end:]


WS_RECEIVE = r'''
void WebSocket::OnTcpData(const std::string& data) {
    if (xEventGroupGetBits(handshake_event_group_) & HANDSHAKE_FAILED_BIT) return;
    auto failed = [this](NetworkError error = NetworkError::ProtocolError()) {
        if (xEventGroupGetBits(handshake_event_group_) & HANDSHAKE_FAILED_BIT) return;
        connected_ = false;
        xEventGroupSetBits(handshake_event_group_, HANDSHAKE_FAILED_BIT);
        if (on_error_) on_error_(error);
        if (on_disconnected_) on_disconnected_();
        // The owner task tears down TCP. Never self-wait in a receive callback.
    };
    family::c3::network::Receive(
        receive_buffer_, handshake_completed_, is_fragmented_, is_binary_,
        current_message_, data,
        [this]() { xEventGroupSetBits(handshake_event_group_, HANDSHAKE_SUCCESS_BIT); },
        [this](const char* p, size_t n, bool binary) { if (on_data_) on_data_(p, n, binary); },
        [this, &failed](uint8_t op, const char* p, size_t n) {
            if (op == 8) {
                connected_ = false;
                xEventGroupSetBits(handshake_event_group_, HANDSHAKE_FAILED_BIT);
                if (on_disconnected_) on_disconnected_();
                return false;
            }
            if (op == 9) {
                if (!SendControlFrame(10, p, n)) {
                    failed(NetworkError::TransmitFailed());
                    return false;
                }
            } else if (on_pong_) on_pong_(p, n);
            return true;
        }, failed);
}
'''

TCP_SUPPORT = r'''
// Plain ws only. DNS remains outside this post-resolution connect budget.
static int64_t C3MonotonicUs() {
    timespec now{};
    if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) return -1;
    return int64_t(now.tv_sec) * 1000000 + now.tv_nsec / 1000;
}
static int C3WaitWritable(int fd, int64_t remaining) {
    if (fd < 0 || fd >= FD_SETSIZE) { errno = EBADF; return -1; }
    fd_set writes;
    FD_ZERO(&writes); FD_SET(fd, &writes);
    timeval timeout{static_cast<time_t>(remaining / 1000000),
                    static_cast<suseconds_t>(remaining % 1000000)};
    return select(fd + 1, nullptr, &writes, nullptr, &timeout);
}
static int C3ConnectBounded(int fd, const sockaddr* address, socklen_t size) {
    const int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0 || fcntl(fd, F_SETFL, flags | O_NONBLOCK) < 0) return -1;
    const int64_t start = C3MonotonicUs();
    if (start < 0) { const int saved = errno; fcntl(fd, F_SETFL, flags); errno = saved; return -1; }
    const int64_t deadline = start + family::c3::network::kConnectBudgetUs;
    int result = connect(fd, address, size);
    int saved = errno;
    if (result < 0 && (saved == EINPROGRESS || saved == EALREADY || saved == EINTR)) {
        while (true) {
            const int64_t current = C3MonotonicUs();
            if (current < 0) { saved = EIO; break; }
            const int64_t remaining = deadline - current;
            if (remaining <= 0) { saved = ETIMEDOUT; break; }
            const int ready = C3WaitWritable(fd, remaining);
            if (ready < 0 && errno == EINTR) continue;
            if (ready <= 0) { saved = ready == 0 ? ETIMEDOUT : errno; break; }
            socklen_t error_size = sizeof(saved);
            if (getsockopt(fd, SOL_SOCKET, SO_ERROR, &saved, &error_size) != 0) saved = errno;
            result = saved == 0 ? 0 : -1;
            break;
        }
    }
    if (result == 0) {
        const int64_t completed = C3MonotonicUs();
        if (completed < 0) { saved = EIO; result = -1; }
        else if (completed >= deadline) { saved = ETIMEDOUT; result = -1; }
    }
    if (fcntl(fd, F_SETFL, flags) < 0 && result == 0) { saved = errno; result = -1; }
    if (result < 0) errno = saved;
    return result;
}
'''

TCP_SEND = r'''
int EspTcp::Send(const std::string& data) {
    if (!connected_ || tcp_fd_ < 0) { errno = ENOTCONN; return -1; }
    int error = 0;
    const bool complete = family::c3::network::SendAll(
        data.data(), data.size(), family::c3::network::kSendBudgetUs,
        C3MonotonicUs,
        [this](const char* p, size_t n) -> family::c3::network::WriteResult {
            // Reload the live descriptor for every syscall; do not retain a
            // descriptor captured before the receive task closes the peer.
            if (!connected_ || tcp_fd_ < 0) return {-1, ENOTCONN};
            const int result = send(tcp_fd_, p, n, MSG_DONTWAIT);
            return {result, result < 0 ? errno : 0};
        },
        [this](int64_t remaining) -> family::c3::network::WaitResult {
            if (!connected_ || tcp_fd_ < 0) return {-1, ENOTCONN};
            const int ready = C3WaitWritable(tcp_fd_, remaining);
            return {ready, ready < 0 ? errno : 0};
        }, error);
    if (!complete) { errno = error; return -1; }
    return static_cast<int>(data.size());
}
'''


def websocket(source):
    source = replace_once(source, '#include "web_socket.h"', '#include "web_socket.h"\n#include "c3_websocket_bounds.h"')
    source = replace_once(source, "    handshake_event_group_ = xEventGroupCreate();", "    receive_buffer_.reserve(family::c3::network::kReceiveBytes);\n    handshake_event_group_ = xEventGroupCreate();")
    source = replace_function(source, "WebSocket::~WebSocket()", "void WebSocket::SetHeader", '''
WebSocket::~WebSocket() {
    if (tcp_) { tcp_->Disconnect(); tcp_.reset(); }
    if (handshake_event_group_) vEventGroupDelete(handshake_event_group_);
}''')
    source = replace_once(source, "    return connected_;", "    return connected_ && !(xEventGroupGetBits(handshake_event_group_) & HANDSHAKE_FAILED_BIT);")
    source = replace_once(source, "    connected_ = false;\n    // 使用 tcp 建立连接", "    connected_ = false;\n    handshake_completed_ = false;\n    continuation_ = false;\n    is_fragmented_ = false;\n    receive_buffer_.clear();\n    current_message_.clear();\n    // 使用 tcp 建立连接")
    source = replace_once(source, "        if (connected_) {\n            connected_ = false;", "        if (!(xEventGroupGetBits(handshake_event_group_) & HANDSHAKE_FAILED_BIT)) {\n            connected_ = false;\n            xEventGroupSetBits(handshake_event_group_, HANDSHAKE_FAILED_BIT);")
    source = replace_once(source, "if (tcp_->Send(request) < 0)", "if (tcp_->Send(request) != static_cast<int>(request.size()))")
    source = replace_once(source, "        return Fail(NetworkError::TransmitFailed());", "        xEventGroupSetBits(handshake_event_group_, HANDSHAKE_FAILED_BIT);\n        return Fail(NetworkError::TransmitFailed());")
    source = replace_once(source, "        return Fail(NetworkError::Timeout());", "        xEventGroupSetBits(handshake_event_group_, HANDSHAKE_FAILED_BIT);\n        return Fail(NetworkError::Timeout());")
    source = replace_once(source, "    if (bits & HANDSHAKE_SUCCESS_BIT) {", "    bits |= xEventGroupGetBits(handshake_event_group_);\n    if ((bits & HANDSHAKE_SUCCESS_BIT) && !(bits & HANDSHAKE_FAILED_BIT)) {")
    # Receive failure already notified the owner; do not duplicate OnError.
    source = replace_once(source, "        if (on_error_) {\n            on_error_(err);\n        }\n        return Fail(err);", "        return Fail(err);")
    source = replace_once(source, "bool WebSocket::Send(const void* data, size_t len, bool binary, bool fin) {", "bool WebSocket::Send(const void* data, size_t len, bool binary, bool fin) {\n    std::lock_guard<std::mutex> lock(send_mutex_);\n    if (!tcp_ || !IsConnected()) return false;")
    source = replace_once(source, "bool WebSocket::SendControlFrame(uint8_t opcode, const void* data, size_t len) {", "bool WebSocket::SendControlFrame(uint8_t opcode, const void* data, size_t len) {\n    std::lock_guard<std::mutex> lock(send_mutex_);\n    if (!tcp_ || (!connected_ && !handshake_completed_) ||\n        (xEventGroupGetBits(handshake_event_group_) & HANDSHAKE_FAILED_BIT)) return false;")
    source = source.replace("    std::lock_guard<std::mutex> lock(send_mutex_);\n    return tcp_->Send(frame) >= 0;", "    return tcp_->Send(frame) == static_cast<int>(frame.size());")
    source = replace_function(source, "void WebSocket::OnTcpData", "bool WebSocket::SendControlFrame", WS_RECEIVE)
    source = replace_function(source, "void WebSocket::Close()", "void WebSocket::OnConnected", '''
void WebSocket::Close() {
    if (IsConnected()) SendControlFrame(0x8, nullptr, 0);
    // Owner-thread cleanup also handles a prior receive failure/passive close.
    if (tcp_) tcp_->Disconnect();
    connected_ = false;
    xEventGroupSetBits(handshake_event_group_, HANDSHAKE_FAILED_BIT);
}''')
    return source


def tcp(source):
    source = replace_once(source, '#include "esp_tcp.h"', '#include "src/esp/esp_tcp.h"\n#include "c3_tcp_deadline.h"\n#include <fcntl.h>\n#include <sys/select.h>\n#include <time.h>')
    source = replace_once(source, 'static const char *TAG = "EspTcp";', 'static const char *TAG = "EspTcp";\n' + TCP_SUPPORT)
    source = replace_once(source, "int ret = connect(tcp_fd_, (struct sockaddr*)&server_addr, sizeof(server_addr));", "int ret = C3ConnectBounded(tcp_fd_, (struct sockaddr*)&server_addr, sizeof(server_addr));")
    source = replace_function(source, "int EspTcp::Send", "void EspTcp::ReceiveTask", TCP_SEND)
    source = replace_once(source, "    if (tcp_fd_ != -1) {\n        close(tcp_fd_);", "    if (tcp_fd_ != -1) {\n        shutdown(tcp_fd_, SHUT_RDWR); // unblock recv before joining its task\n        close(tcp_fd_);")
    source = source.replace("pdMS_TO_TICKS(10000)", "portMAX_DELAY")
    return source


def generate(component_root, output_dir):
    root = Path(component_root).resolve()
    out = Path(output_dir).resolve()
    if out == root or root in out.parents:
        raise ValueError("output must be outside the immutable component root")
    original = {}
    for name, expected in ORIGINAL_SHA256.items():
        path = root / name
        if path.is_symlink():
            raise ValueError(f"source symlink refused: {name}")
        data = path.read_bytes()
        if sha(data) != expected:
            raise ValueError(f"original SHA256 mismatch: {name}")
        original[name] = data
    helper_root = Path(__file__).resolve().parents[1] / "main" / "audio"
    helpers = {name: sha((helper_root / name).read_bytes()) for name in HELPERS}
    sources = {"web_socket.cc": websocket(original["src/web_socket.cc"].decode()),
               "esp_tcp.cc": tcp(original["src/esp/esp_tcp.cc"].decode())}
    prefix = "// Generated C3-only build copy; Apache-2.0 upstream esp-ml307 3.7.6.\n// Source/provenance and recipe hashes: family_c3_network.manifest.json\n"
    encoded = {name: (prefix + body).encode() for name, body in sources.items()}
    manifest = {"schema": 1, "source_uri": SOURCE_URI, "source_commit": SOURCE_COMMIT,
                "archive_sha256": ARCHIVE_SHA256, "component_hash": COMPONENT_HASH,
                "license": "Apache-2.0", "original_sha256": ORIGINAL_SHA256,
                "patched_sha256": {name: sha(data) for name, data in encoded.items()},
                "helper_sha256": helpers, "recipe_sha256": sha(Path(__file__).read_bytes()),
                "profile": "CONFIG_XMINI_C3_PCM; plain ws only; DNS outside connect budget"}
    # All hashes and anchors are checked before creating or writing outputs.
    out.mkdir(parents=True, exist_ok=True)
    for name in (*encoded, "family_c3_network.manifest.json", "LICENSE.esp-ml307"):
        if (out / name).is_symlink():
            raise ValueError(f"output symlink refused: {name}")
    for name, data in encoded.items():
        (out / name).write_bytes(data)
    (out / "LICENSE.esp-ml307").write_bytes(original["LICENSE"])
    (out / "family_c3_network.manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--component-root", required=True)
    parser.add_argument("--output-dir", required=True)
    args = parser.parse_args()
    generate(args.component_root, args.output_dir)


if __name__ == "__main__":
    main()

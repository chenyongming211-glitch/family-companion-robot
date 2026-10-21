# 软件播放参考的有界采样历史

> **For agentic workers:** Use superpowers:subagent-driven-development. The user approved the natural-barge-in route and asked to finish confirmed code/build/automated-test work quietly while resting. No new approval is needed for this prerequisite.

**Goal:** 给后续 DMA 适配器提供按采样序号读取的固定容量参考历史，拒绝缺口、过期和跨代数据。

**Architecture:** 纯 C++17 数据结构放在 StopWatch 的受版本控制 overlay 中；实际 codec 在后续任务持有 ISR/任务共用锁调用它。这个前置任务只实现可测试的数据语义，不选择或假装完成 DMA 时间对齐、AEC、实时上传或固件刷写。

**Tech Stack:** C++17、标准库、Python unittest 编译并执行真实主机 C++ 回归。

## Global Constraints

- 在 `/Users/chen/code/家庭陪伴机器人` 的 `main` 直接开发、提交；不建分支或 worktree。
- 用户正在休息。不发进度消息或问题，不打开串口，不刷机、重启设备、重启/部署线上网关或采集现场音频。
- PCM 容量固定为 16000 个 int16 样本（32000 字节），整个对象不得超过 32768 字节；无动态分配、无内部阻塞锁。
- 调用方必须用同一个 ISR 安全的临界区同步 Reset/Append/Read 和状态读取。此结构本身不宣称线程安全。
- 不能把参考缺失当成静音补零。只有明确 Append 的零样本可以读出为零。
- 播放中断不重置参考历史；已提交 DAC 的尾音仍需读取。Reset 只用于硬件采样时间轴重新开始，换代后旧窗口必须失效。
- 不修改解压后的 vendor/generated 源码；不把密钥、备份、录音、编译输出或 `.superpowers/` 加入 Git。

### Task 1: Sample-indexed playback reference history

**Files:**
- Create: `firmware/overlays/main/boards/m5stack/stopwatch/playback_reference_buffer.h`
- Create: `firmware/tests/playback_reference_buffer_test.cc`
- Create: `firmware/tests/test_playback_reference.py`
- Modify: `firmware/README.md` (document this prerequisite's scope and locking contract)

**Interfaces:**
- Class `PlaybackReferenceBuffer` in namespace `family_robot`.
- `Reset(uint32_t generation)` empties logical history and starts sample index 0 without clearing/allocating storage.
- `Append(uint32_t generation, int64_t first_sample, const int16_t* pcm, size_t count)` accepts only the current generation and contiguous samples. On invalid input returns false without mutation. The ring retains the newest 16000 samples; consumers detect expiry through Read.
- `Read(uint32_t generation, int64_t first_sample, int16_t* output, size_t count) const` returns Ready, NotYetAvailable, Expired, DifferentGeneration or InvalidRange. Output is entirely unchanged on every non-Ready result.
- `begin_sample()`, `end_sample()`, `generation()` and `size()` are diagnostic accessors subject to the same caller locking contract.

- [x] **Step 1: Write the behavioral C++ harness and Python runner before the header.**

The C++ harness uses real sample arrays and sentinels and must cover these independent cases: exact append/read; read across block boundaries; wrap after more than capacity with distinct ramp samples; expired read leaves output unchanged; future/incomplete read leaves output unchanged; wrong generation on both append/read leaves history/output unchanged; append gap and overlap rejected without changing the prior window; explicit zero samples readable without fabricating missing samples; negative index/null buffer/count above capacity rejected; signed sample-index overflow rejected; Reset invalidates prior history while preserving the chosen new generation; a submitted tail followed by zeros remains readable (no reset on playback stop). Use `static_assert(sizeof(PlaybackReferenceBuffer) <= 32768)`.

Use this full test shape and expand the named scenarios above with actual sample assertions:

```cpp
#include "playback_reference_buffer.h"
#include <array>
#include <cstdint>
#include <iostream>
#include <limits>
#include <stdexcept>
#include <vector>

using family_robot::PlaybackReferenceBuffer;
using Status = PlaybackReferenceBuffer::ReadStatus;
static_assert(sizeof(PlaybackReferenceBuffer) <= 32768);

static void Check(bool condition, const char* message) {
    if (!condition) throw std::runtime_error(message);
}

static void ExactAndSplitWindow() {
    PlaybackReferenceBuffer buffer;
    buffer.Reset(7);
    const int16_t a[] = {11, 12, 13};
    const int16_t b[] = {21, 22, 23};
    Check(buffer.Append(7, 0, a, 3), "first block rejected");
    Check(buffer.Append(7, 3, b, 3), "second block rejected");
    std::array<int16_t, 4> result{};
    Check(buffer.Read(7, 1, result.data(), result.size()) == Status::Ready,
          "split window unavailable");
    Check((result == std::array<int16_t, 4>{12, 13, 21, 22}), "split sample order");
    Check(buffer.begin_sample() == 0 && buffer.end_sample() == 6 && buffer.size() == 6,
          "wrong timeline bounds");
}

int main() {
    try {
        ExactAndSplitWindow();
        // Call each other named scenario implemented above main.
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
```

The comment in this example is a harness insertion point, not a completed test: implement and invoke all scenarios before adding production code. For overflow, a full-capacity Read starting at `INT64_MAX - 1` must return InvalidRange without copying. A huge Append count such as `SIZE_MAX` must be rejected without dereferencing input; this tests signed arithmetic guards without adding a production-only counter setter.

The Python runner must compile this real harness once in a temporary directory and execute it, with captured compiler/runtime diagnostics used on assertion failure:

```python
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]

class PlaybackReferenceTest(unittest.TestCase):
    def test_cpp_behavior(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / "reference-test"
            compile_result = subprocess.run([
                "c++", "-std=c++17", "-Wall", "-Wextra", "-Werror", "-pedantic",
                "-I", str(ROOT / "firmware/overlays/main/boards/m5stack/stopwatch"),
                str(ROOT / "firmware/tests/playback_reference_buffer_test.cc"),
                "-o", str(binary),
            ], capture_output=True, text=True)
            self.assertEqual(compile_result.returncode, 0, compile_result.stderr)
            result = subprocess.run([str(binary)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
```

Run `python3 -m unittest discover -s firmware/tests -p test_playback_reference.py -v`; record expected missing-header compilation failure. No source-text assertions or mock ring implementations.

- [x] **Step 2: Implement the bounded buffer header.** Use this complete implementation contract, preserving exact API names:

```cpp
#pragma once

#include <array>
#include <cstddef>
#include <cstdint>
#include <limits>

namespace family_robot {

// Caller synchronizes every operation with one ISR/task-safe critical section.
// Indices describe the playback sample timeline, not arrival or network time.
class PlaybackReferenceBuffer {
public:
    static constexpr size_t kCapacity = 16000;
    enum class ReadStatus {
        Ready,
        NotYetAvailable,
        Expired,
        DifferentGeneration,
        InvalidRange,
    };

    PlaybackReferenceBuffer() = default;
    PlaybackReferenceBuffer(const PlaybackReferenceBuffer&) = delete;
    PlaybackReferenceBuffer& operator=(const PlaybackReferenceBuffer&) = delete;

    void Reset(uint32_t generation) {
        generation_ = generation;
        begin_ = 0;
        end_ = 0;
    }

    bool Append(uint32_t generation, int64_t first_sample, const int16_t* pcm,
                size_t count) {
        if (generation != generation_ || first_sample != end_ ||
            !ValidRange(first_sample, count) || (count != 0 && pcm == nullptr)) {
            return false;
        }
        for (size_t i = 0; i < count; ++i) {
            const auto sample = first_sample + static_cast<int64_t>(i);
            samples_[static_cast<size_t>(sample % static_cast<int64_t>(kCapacity))] = pcm[i];
        }
        end_ += static_cast<int64_t>(count);
        if (end_ - begin_ > static_cast<int64_t>(kCapacity)) {
            begin_ = end_ - static_cast<int64_t>(kCapacity);
        }
        return true;
    }

    ReadStatus Read(uint32_t generation, int64_t first_sample, int16_t* output,
                    size_t count) const {
        if (generation != generation_) return ReadStatus::DifferentGeneration;
        if (!ValidRange(first_sample, count) || (count != 0 && output == nullptr)) {
            return ReadStatus::InvalidRange;
        }
        if (first_sample < begin_) return ReadStatus::Expired;
        if (first_sample + static_cast<int64_t>(count) > end_) {
            return ReadStatus::NotYetAvailable;
        }
        for (size_t i = 0; i < count; ++i) {
            const auto sample = first_sample + static_cast<int64_t>(i);
            output[i] = samples_[static_cast<size_t>(sample % static_cast<int64_t>(kCapacity))];
        }
        return ReadStatus::Ready;
    }

    int64_t begin_sample() const { return begin_; }
    int64_t end_sample() const { return end_; }
    uint32_t generation() const { return generation_; }
    size_t size() const { return static_cast<size_t>(end_ - begin_); }

private:
    static bool ValidRange(int64_t first_sample, size_t count) {
        return first_sample >= 0 && count <= kCapacity &&
               static_cast<int64_t>(count) <=
                   std::numeric_limits<int64_t>::max() - first_sample;
    }

    std::array<int16_t, kCapacity> samples_{};
    int64_t begin_ = 0;
    int64_t end_ = 0;
    uint32_t generation_ = 0;
};

static_assert(sizeof(PlaybackReferenceBuffer) <= 32768);

}  // namespace family_robot
```

If a real behavioral test exposes a defect in this starting implementation, fix the defect rather than weakening the test; report the correction. This class does not own calibration, clocks, DSP, reset-on-interruption policy, thread scheduling or application actions.

- [x] **Step 3: Verify the new contract and existing preparation regressions once.**

```sh
python3 -m unittest discover -s firmware/tests -p test_playback_reference.py -v
python3 -m unittest discover -s firmware/tests -v
git diff --check
```

Expected: real C++ behavior passes; 19 existing preparation tests remain passing. Do not rerun unrelated Go tests for these files. The existing CI's firmware-test discovery will execute this new harness on Linux when the controller pushes.

- [x] **Step 4: Document, self-review and commit on main.** Add a concise README section: reference-history scope, exact 32000-byte PCM/32768-byte object bounds, caller locking, no invented silence, reset only for a hardware timeline restart, and no AEC/firmware integration yet. Record RED/GREEN commands, actual scenarios, memory size and findings in the assigned ignored report. Stage only four task files and commit, without push.

## Remaining approved work

This prerequisite does not finish natural barge-in. The next implementation must connect actual TX/RX DMA sample positions to MR input, measure timing, implement fail-closed fallback and explicit interruption queue handling, build the experimental variant and prepare rollback. Hardware operation and field acceptance wait for the user tomorrow.

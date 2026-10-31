# Xmini C3 Button Entry Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development for isolated implementation tasks and sequential specification/code-quality review. All tasks use the existing main checkout; no branches or worktrees.

**Goal:** 接入 Xmini C3 的按键开启连续语音、实际播放回报及独立设备身份；播放中暂停上行，完成后恢复。

**Architecture:** 保留现有 Go 18041 → 千问 Realtime。以锁定上游的 xmini/c3 为硬件基础，新增产品 variant、C3 专用 ES8311 DMA 播放适配与按实际播放状态控制的上行门。复用现有 playback 协议、设备鉴权和记忆，不降低已有准入要求。

**Tech Stack:** ESP-IDF 6.1、ESP32-C3、ES8311、SSD1306、C++17、Python host harness、Go、GitHub Actions。

## 1. 固定来源和设备回退资料

- [x] 保存正式服务 health/PID/NRestarts 和保护配置摘要到云端私有工作目录。
- [x] 云端独立目录下载并验证 `firmware/upstream.lock.json` 的源码归档 SHA256，读取 Xmini/ES8311/应用状态切换源码。
- [x] Mac 只读目标 ROM/安全状态，完整读取 16MB 到 `.superpowers/c3-inspection-20261031/` 的私有子目录；核对分区表、OTA 选择与 eFuse 既有状态。只输出无秘密元数据。
- [x] 使用 `writing-plans` 完成本计划，并以用户已选定的规格直接推进，不重复询问是否继续。

## 2. C3 播放驱动与输入门控

Files: create `firmware/overlays/main/boards/xmini/c3/family_c3_codec.h`, `.cc` and focused `.inc` files; create `firmware/tests/c3_codec_host.cc`, `test_c3_codec.py`; create `firmware/patches/0010-xmini-c3-button-entry.patch`.

- [x] 先写生产 adapter host harness，覆盖回报、生命周期、实际 DMA、失败与重连；云端执行 `python3 -m unittest discover -s firmware/tests -p test_c3_codec.py -v`，确认缺失实现失败。
- [x] 实现 `FamilyC3Codec : public Es8311AudioCodec` 的 `PlaybackSupported/Control/Generation/Poll/Busy/OutputTagged`，复用 `family_robot::PlaybackProgress`。使用 6×240 帧 mono16/24k DMA，回调拥有输出；格式或硬件失败则回报无效。
- [x] 先测试播放期/排空/断连/旧编码帧的上行开关，再新增仅 C3 variant 使用的门控。使用现有连接/回复状态与物理完成事实，避免依赖 `tts.stop` 提前恢复。
- [x] 云端重跑 focused harness 的 O2 与 ASan/UBSan；独立规格审查后进行代码质量审查，修复确定问题。

## 3. 板型、依赖与证据

Files: modify `firmware/tools/prepare_experiment.py`, `verify_experiment.py`, `.github/workflows/firmware-experiment.yml`; add `firmware/dependencies.esp32c3.lock` and `.sha256`, `firmware/overlays/partitions/xmini-c3-family-16m.csv`; extend evidence tests and `firmware/README.md`.

- [x] 新增独立 `xmini-c3-family` variant，选正确 board/target/USB console，禁用 wake/AFE/AEC/orb；按键开关会话，保留 OLED 与 ES8311 引脚。
- [x] 移除仅产品 variant 的自动 eFuse 写入；普通上游 variant 代码保持。
- [x] 用独立云端构建阶段生成 C3 native dependency candidate，审计 target 条件差异和关键版本后固定字节/摘要；最终正常构建不接受 lock 重写。
- [x] 制备工具增加 `--target esp32c3`，其余默认保持 esp32s3。验证器按目标锁核对 provenance 与 native lock 字节，分别核对 C3 无 PSRAM/AFE/唤醒与正确分区。
- [x] 先写 verifier 的错误 target、错误锁、缺播放能力、错误板型/地址/镜像容量拒绝用例，再实现。
- [x] 云端运行全 firmware host suite、上游脚本测试；提交推送 main，Actions 构建新 C3 和四旧目标。下载精确提交/variant 产物，核验镜像、分区、依赖、配置和 SHA256。

## 4. 独立设备登记与 USB 安装

- [x] 在服务器凭据表新增实际 Device-Id 与唯一 token，保留所有既有条目；准备 OTA 引导与回退，绝不输出 token。
- [x] 核对当前设备/固件分区兼容性，明确写入区和预期前后摘要；必要时迁移资源但保留 NVS/phy/storage/身份。
- [x] 用户网络资料可用时通过 USB 配置；不切换 Mac 到设备热点、不把网络凭据写进 Git。
- [x] 云端服务空闲时加载新凭据快照；仅在最终候选全部验证后刷写该 C3。启动前回读全部改动区与保留区，确认一致后才运行新应用。登记属主错误与初次串口重连回退均已处理；最终指定Wi-Fi、实际OTA VALID、激活及再次启动确认通过。

## 5. 端到端与交接

- [x] 云端验证该设备鉴权、跨设备凭据拒绝、真实模型/真实DMA完成回报、记录/近期记忆写入归属；原设备鉴权与构建回归通过，未自动操作原硬件。
- [x] 实机按键进入对话，OLED/codec初始化及两轮上行/物理播放计数通过。用户确认两轮完整并可按键结束；95040/252000样本均sent=accepted=completed，i2s_dma/played。追加同C3重连加载2轮记忆、7个回答全量播放的后台证据；无新增逐轮真人反馈。
- [ ] 重复结束/重连、音量实际调节、跨会话回忆、断网和长时间待机作为后续硬件验收；本轮两轮正常不外推这些结果。
- [x] 更新 `docs/verification/2026-10-31-xmini-c3-button-entry.md` 与 `docs/DEVELOPMENT_HANDOFF.md`，区分实现、目标构建、安装和实机结果，列出剩余限制；随本轮收尾提交推送 main。

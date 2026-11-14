# 音频首次故障定位与受控恢复实施计划

> **For agentic workers:** 使用superpowers:subagent-driven-development或superpowers:executing-plans逐项执行。用户已明确批准此前讨论范围并要求开发，不重复申请开发许可；直接main，不建分支或worktree。

**Goal:** 保存首次音频故障，允许有自检依据的一次受控恢复，并立即反馈音频/握手失败。

**Architecture:** 小型固定故障元数据＋现有Waveshare音频生命周期恢复＋共用协议失败等待；保留当前播放证据、能力协商和24kHz路径。

**Tech Stack:** 既有C++/ESP-IDF6.1、NVS、Python主机夹具；云端测试/编译，无新依赖。

**Spec:** [设计](../specs/2026-11-14-audio-fault-recovery-design.md)。

## Global Constraints

- main直接开发/提交/推送；Mac代码/Git/USB客户端，所有运行测试和构建在云端。
- 仅微雪ESP32-S3 2.16验收；现有历史来源保留，无新增M5/C3任务。
- 每启动至多一次恢复、一次首次故障NVS保存；不整机自动重启、不放宽原音频校验或服务端能力门槛。
- 恢复前锁等待最多200ms；静音DMA自检至少12次、等待最多500ms；有界真实输入读取；失败继续隔离。
- 不改字体/资源/分区或凭据；app增加≤16384B、各槽余量≥65536B，原槽3604480B。
- 原音/凭据/硬件快照/生成固件不入Git。实机安装与验收不由自动化成绩代替。

## Review Focus

- 首次故障在DMA ISR发生，任务线程同时查询；快照必须一致且不在ISR写NVS。
- 恢复中旧ISR/音频工作线程或旧socket回调迟到，不能污染新代次或误报成功。
- SDK重开、DMA不来或输入停滞，操作有界结束并保留保护。
- NVS空间不足/损坏，诊断失败不能重启或擦除设备配置。
- 音乐独占、初始化不完整与一启动第二次故障，不得重复恢复或抢占。

## Task 1：首次故障原因与小型持久记录

**Files:** `firmware/overlays/main/audio/playback_progress.h`、新增故障/存储辅助及对应主机用例；适配器接入由Task2负责。

**Interfaces:** 固定版本故障快照；PlaybackProgress提供首个DMA子原因；TryLoad/TrySave小记录返回成功/失败，不abort。

- [ ] RED：细分DMA节拍/形状/顺序，首因不覆盖；存储坏/满/失败；结构大小有界。
- [ ] 实现无ISR分配的数字原因和固定记录辅助，保留原音频阈值。
- [ ] 云端相应用例GREEN，审阅后提交。

## Task 2：适配器一次受控恢复

**Files:** Waveshare `xiaoman_box_codec.h`、`xiaoman_box_lifecycle.inc`、`xiaoman_box_dma.inc`、新增小型恢复实现、`0025-music-audition.patch`的共用重开接缝、对应主机夹具。

**Interfaces:** `bool AudioCodec::PrepareAudioConnection()`默认正常返回；Waveshare覆盖。首次故障类型来自Task1。正常路径不恢复；曾正常初始化的故障允许每启动一次；音乐独占或初始化不完整明确拒绝。

- [ ] RED：正常路径、运行期故障恢复、真实DMA/输入要求、锁/自检超时、二次故障、旧身份/队列/唤醒批次、音乐独占。
- [ ] 接入首因保存，复用静音/双工重开，在保护状态下验证12回调和有超时读取，全部通过后发布新代次。
- [ ] 云端定向主机GREEN及音乐/播放/采集回归，独立规格与质量复核。

## Task 3：连接前检查与握手快速失败

**Files:** `firmware/tools/prepare_connection_diagnostics.py`及实际协议提取主机夹具；必要小型共用辅助。

**Interfaces:** 调用Task2的`PrepareAudioConnection()`；音频失败显示`音频异常`并拒绝建连。当前连接关闭或非法hello立即唤醒失败等待，旧连接不影响新等待；正常10秒上限保留。

- [ ] RED：实际生成OpenAudioChannel方法，覆盖音频拒绝/正常hello/关闭/非法hello/超时/旧回调。
- [ ] 实现最少事件位和当前代次校验，保留原有网络/鉴权和播放协商语义。
- [ ] 云端协议/连接/捕获用例GREEN与审查，提交。

## Task 4：整体验证、体积与交付

- [ ] 冻结源码到云端独立目录；原生缓存使用前核对既有来源，无其他构建者。
- [ ] 云端支持机型主机全套与最终独立审查，记录真实来源与失败。
- [ ] 精确Waveshare原生构建，比较3517280B基线，验证两槽容量、≥64KiB余量和≤16KiB新增；分区/assets保持。
- [ ] 更新验证报告和交接；main提交推送并核对远端及干净工作区。
- [ ] 仅在具备现场安装条件时另行执行有备份/回读的安装及真人复验；没有现场结果则明确保留待验。

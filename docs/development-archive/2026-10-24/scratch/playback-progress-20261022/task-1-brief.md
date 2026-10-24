### Task 1: 固件完成边界、身份与回报

**Files:** 新增 `firmware/patches/0002-playback-progress.patch`；修改软件 AEC codec overlays；新增独立可主机测试的固定容量 DMA 跟踪头及 C++/Python 测试。必要时新增固件原生集成测试、配置和验证元数据。

**Consumes:** 线协议；现有 `SoftwareAecCodec::Complete` 与 `DuplexReference`；固定 IDF 实现。
**Produces:** 具备协商、逐帧归属、200ms进度和最终回报的可编译固件源码；普通对话降级可用；源码测试与实际目标编译分开报告。

- [ ] 读完整 codec/音频任务/协议/AEC 路径与固定 SDK `i2s_dma_tx_callback`。设计并说明每一份 PCM 的身份如何到达真实完成回调，不能以完成总计或 PCM 相似度匹配代替归属。
- [ ] 首选使用 SDK 已支持的 DMA 回调重填模式：软件 AEC 变体禁用 callback 后自动清零，由固定容量 PCM 队列在完成回调中先记录旧 DMA 内容与身份，再填下一轮 PCM 或零。不调用 `i2s_channel_write` 同时写这些 DMA；原始变体维持原路径。若采用其他实现，必须提供同等严格的归属证明，不修改 SDK 外部文件。
- [ ] 使用小而明确的核心接口，例如 `Submit(identity, first_sample, pcm, count)`、`Complete(dma_address, completed_pcm, count)`、`Seal(identity, interrupted)`、`Snapshot()`；身份至少包含本地连接代次与服务端播放代号。线程包装与纯状态机分开，ISR固定容量、无分配/网络/日志/嵌套锁。
- [ ] 先写并跑失败用例：提交后无完成不增长；6个 DMA 槽轮转、非零与全零语音、提示音/空档不计数；中断清队列与 DMA 尾部；正常封口排空；旧代次、乱序/遗漏、格式/写入失败；临时断流；新回复紧随中断；硬件重开及计数边界。C++测试必须调用实际生产核心，不能只搜索源码。
- [ ] 把身份从接收帧贯穿到在途解码/输出，处理 TTS start 与异步主线程状态切换、stop/reset/new start 的竞态。队列满/解码错误使该块位置无效；不能借下一块代号重标旧任务。
- [ ] 发送回报走正常任务，有界且有连接身份检查；在原始服务器上不带回报也能正常唤醒、对话和控制。验证 esp_codec_dev 输出音量路径是否包含必要处理；如绕过写入，明确保证硬件音量行为并保留 AEC 的物理参考。
- [ ] 跑 `python3 -m unittest discover -s firmware/tests -v`；新鲜准备树并跑上游脚本测试。记录新增 SRAM 上界及锁/回调路径，再提交自己的文件到 main，不推送。
- [ ] 独立规格与代码评审；修复后再评审。只有无未处理重要问题时进入任务2。


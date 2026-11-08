# CHICK-01 表情接入实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用用户指定的CHICK-01参数化脸替换小光球，先配置现有微雪研发机。

**Architecture:** 纯C++参数/状态模型和RGB565渲染器与LVGL适配分离；复用当前代次与DMA完成音频观察。设备协商face_state_v1后接收已有Go思考提示；旧音频收发、排空与唤醒基线保持。

**Tech Stack:** 锁定ESP-IDF6.1、LVGL9、ESP32-S3、Go；Mac仅代码/Git/USB客户端，测试与构建在云端执行。按用户要求直接main，不创建worktree。

## Task 1：参数、模型与渲染器

Files: 新增firmware/overlays/main/display/chick_face_model.h、chick_face_renderer.h及firmware/tests/chick_face_host.cc、test_chick_face.py。

- [x] 先写host覆盖：19个情绪别名和未知词、27组参数、实际DMA嘴型、旧capture/connection不可复用、系统状态优先、低亮仍显示新脸、情绪超时/防抖及绘图内存哨兵。
- [x] 将测试上传云端私有目录；运行新测试获得缺少实现的RED，再实现并用ASan/UBSan运行GREEN。
- [x] 输出同一实际渲染器的PPM预览，主任务检查其图形，不能以参考网页截图替代固件绘图。

固定接口：namespace family_robot，FaceState十二项加Hidden；FaceEmotion含网页15项加Thinking复用；FaceFacts包含now_us、state、screen_on、quiet、system_page、low_battery、charging、model_thinking和OrbAudioActivity audio。FaceModel::SetEmotion(const char*,int64_t)、Update(const FaceFacts&)返回FaceFrame；FaceRenderer::kWidth=240、kHeight=320、kBytes=153600，Render(uint16_t*,const FaceFrame&)有界无堆分配。帧包含state、emotion、preset、mouth_level、blink、gaze_x/y、breath、tilt、motion_ms、shown、draining和Caption()。字段单位与网页一致，动画不得改音频事实。

## Task 2：显示适配与产品选择

Files: 新增display/chick_face_view.h/.cc、chick_face_display.inc和后续补丁；移除旧orb view/render/model；保留audio/orb_activity.h及现有DMA观察。更新firmware/tools/verify_experiment.py、相关固件测试和README。

- [x] 将产品配置改为CHICK_FACE_UI，两个小满S3目标选新脸，非小满和C3保持原选择。
- [x] LVGL适配负责PSRAM画布、布局、字体现有锁、实际亮度和降频；资源不足回静态脸。系统预览与提示页面继续拥有最高显示权限，不回旧emoji。
- [x] 用新增WAVESHARE_WAKE_SPEECH_WINDOW默认n隔离未验收0021；lab显式启用，host仍覆盖实验方法。
- [x] 完成显示初始化前调用、低亮、亮度零、通知/预览及输入/输出代次回归；在云端运行全部firmware/tests。
- [x] L0十二系统状态均配实际状态文字；L1情绪、L2微动画不显示名称，情绪切换不得覆盖状态文字，预览/屏幕关闭时一起隐藏。

## Task 3：真实思考提示

Files: internal/realtimelive/xiaozhi_wire.go、xiaozhi_entry.go、相应测试，固件hello能力补丁。

- [x] 测试未协商终端无新增消息、已协商thinking/listening分别映射llm消息的state字段（独立于emotion）、session_id正确且无TTS/音频副作用；终止/取消清除等待提示。
- [x] 实现仅限视觉元数据的能力协商及映射，模型调用和实际播放协议保持；云端Go定向race及全仓检查。
- [ ] 核对正式58c14cd-window600-release源码；如需发布只携带该最小视觉增量，保持600ms设备选择器与正式配置，空闲部署、健康检查与回退产物齐备。

## Task 4：审查、构建和配置设备

- [x] 规格与质量分两阶段独立审查，修复后复核；只对代码更新或新失败补跑必要测试。
- [ ] main提交推送；等待绑定准确SHA的目标产物成功，核对配置、分区、原资源语义兼容与没有lab标记。
- [ ] 以完整app槽预期摘要和原微雪MAC校验后app-only安装，校验回读、保护区与NVS；未连接设备时保留可直接安装的产物与准确阻塞条件。
- [ ] 被动检查UI启动、待机/低亮、实际播放观察与绘制预算；用最少现场配合确认视觉。记录已验与未验，更新交接、取消旧小光球设计的有效状态，再提交推送main。

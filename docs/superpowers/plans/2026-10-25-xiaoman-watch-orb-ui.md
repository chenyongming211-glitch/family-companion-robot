# 小满手表流体光球实施计划

> **For agentic workers:** Use superpowers:subagent-driven-development to implement and review the firmware tasks. User explicitly authorizes direct main development and device installation; do not create branches or worktrees.

**Goal:** 将已确认的B流体光球、简短状态及默认隐藏对话字幕实现到小满StopWatch固件，保留现有小满唤醒、AEC、持续收音、插话排空和云端记录。

**Architecture:** 仅小满variant启用板级LVGL会话视图。便携状态/绘图代码与真实音频快照分离；任务计算幅度，DMA完成仅更新有界标量，UI低优先级读取最新快照。待机和系统页面沿用原显示；不为缺少的服务端话尾事件伪造等待回复状态。

**Tech Stack:** 锁定小智v2.5.2、ESP-IDF6.1、ESP-SR2.4.7、LVGL、C++、现有云端主机测试和三variant Actions。Mac仅代码/Git、查看视觉结果、USB备份及刷机。

## 1. 范围及暂停事项

- [x] 阅读最新交接、设计和现有0001–0006补丁，确认main干净基线2c234f0。
- [x] 用户要求将音频保护锁存故障暂停排查，仅保留记录；本轮不加入自动恢复、不调整阈值或上行保护。
- [x] 核对466×466圆屏、RoundLcdDisplay、显示锁、板级自动源文件收集和当前USB。

## 2. 固件实现与有意义的回归

**Files:** 新增`firmware/patches/0007-xiaoman-orb-ui.patch`；新增板级光球视图、便携状态/绘图及音频观察overlay；最小改动`playback_progress.h`、`software_aec_playback.inc`和必要的元数据读取接口；新增`firmware/tests/test_orb_ui.py`及C++harness；更新`firmware/tools/verify_experiment.py`的variant断言。

- [x] 先写状态/绘图/实际完成包络的回归，在云端观察缺少实现时失败；测试用合成PCM，无模型/设备依赖。
- [x] 隔离小满flag：原版与软件AEC版保持现有界面；小满默认隐藏user/assistant字幕，系统通知、配网、升级和低电量页仍可见。
- [x] 实现近黑底、名称、青蓝紫粉流体光球和一条短状态；15fps目标、预分配PSRAM为主，不逐帧分配，不整屏软件模糊。
- [x] 可聆听状态以实际上行门为准；播放以当前有效DMA完成观察为准；排空提示只在真实封口/排空期间保留；缺乏真实等待事件时维持聆听。
- [x] 输入包络来自当前有效收音代次的AFE处理后音频；输出包络在普通任务计算，随真实完成的DMA块释放；UI不持有PCM、不操作收发队列。
- [x] 检查代次、过期快照、断连、重连、退出、屏幕关闭/待机；旧结果不得恢复新会话的说话动效。
- [x] 提供动效关闭/低帧率/静态回退以支持同机对照；低内存失败仍保留可读状态与原语音功能。
- [x] 云端回归通过，并从生产绘图代码输出正常及异常状态图像供视觉核对。

## 3. 两阶段独立审查

- [x] 先按已批准设计审查状态语义、系统页面优先级、视觉预算和范围；修复确定缺口。
- [x] 再审查锁顺序、ISR工作量、生命周期、内存边界和旧variant隔离；修复后复验。

## 4. 固定SDK构建

- [ ] 云端运行固件全部host测试和新鲜准备源码的上游脚本测试；保留ASan宿主注入的既有适配，不污染Python宿主。
- [ ] main提交推送触发既有三variant CI；核对SDK commit、依赖字节、有效配置、源补丁和产物SHA256。
- [ ] 图形/UI目标编译和固件分区大小分别核验，不用主机测试替代目标构建。

## 5. 备份、安装与实际验证

- [ ] 刷机前通知用户，唯一USB由主代理操作；保存当前app与启动/配置区域私有快照，不刷NVS/otadata。
- [ ] 先比较新旧assets；一致时只刷当前OTA app分区，读取回验，并核对启动/配置区域没有被写入。
- [ ] 实机验证待机、唤醒、连接、收音、回答、插话排空、会话退出；对话正文不显示、云端仍记录。
- [ ] 与动效关闭模式对照基本语音、播放回报、AEC故障/溢出和资源；保留帧率/功耗/长稳的实测边界。
- [ ] 保留已验证54ec169 app及配套assets作为回退，不覆盖Wi-Fi与设备身份。

## 6. 交付

- [ ] 更新设计状态、交接和验证报告，列明目标构建/实机/真人听感分别通过到哪一步，main提交推送并核对远端。
- [ ] 保持现有云服务可用；不重启服务来部署纯固件UI，不新增后台业务或重新开启暂停的音频故障排查。

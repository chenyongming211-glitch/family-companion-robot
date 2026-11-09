# 当前微雪迁移准备与备份

用户已选择“用当前微雪，备份后迁移”，并明确手机预览暂时不方便。因此先完成备份、后端和匹配安装包，实刷与手机重新配网一起后置；不能记为扫码/BLE或实刷已通过。

## 已实现并发布

`762e597` 增加可信 `migrate-legacy`、原绑定关联及同家庭重新确认，保留设备 ID、资料、原绑定起点和账号/正文权限。新认证仍须独立设备确认；普通供应继续拒绝旧 MAC。`f2f8db5` 提供固定回环的 Mac USB 字节客户端，经既有 SSH 反向转发连接云端 esptool，不保存载荷。

云端精确修改源摘要与 checkout 的 7 份 Go 文件一致。管理与 CLI 完整 race 为 78 个顶层测试、238 含子测试，零失败/跳过；vet/build 无诊断、退出 0。USB 四项通过全字节/IAC、参数控制、线程结束、受限 TCP 缓冲区 1 秒背压和 USB 故障关闭；旧 500ms 超时明确截断的 RED 后，10 秒超时 GREEN。规格/质量、USB 修正及整体代码/集成复审均通过；整体结论限后端与安装准备，现场关闭检查另记。新后端变化未触发固件路径 CI；既有 c22 候选的五项 S3/stock CI 证据仍属于 c22。

仅管理服务发布 `onboarding-migration-20261109-r1`，PID2866837，实际进程二进制 SHA256 `bfe6c323cfc756fb27d153caa2b16e1b87497cd5e9f22c0d880b1471000c42dd`；原 dist-live 保持。语音仍 onboarding-20261109-r1/PID2556943、原二进制与参数，ready/memory_ready、active0；未重启语音。公网 session/families/onboarding 未登录 401，非开放 devices/internal-auth 路径仍 404。

7 份受保护配置/身份摘要与 312 份源记录/记忆的集合、摘要、大小、mtime 保持。正式管理配置仍 1 设备/1 原绑定、factory_devices0、claims0；绑定 `binding-home-1` 起点仍 `2026-11-09T02:05:08.453461251Z`。原认证注册表未修改。

## 已准备的私有安装输入

仅在云端配置副本上执行真实目标身份核对及迁移供应，实际旧 MAC/认证摘要/voiceidentity/原设备及有效绑定均匹配。只追加待切换工厂认证，副本原账号、家庭、设备、绑定、claims 数组逐项保持；尚未写正式权威配置。

同一私有包生成官方 develop 环境 280×280 设备码，转为 9848 字节 FRC1；生成 24576 字节微雪 NVS。官方 NVS 解析器逐页/条目 CRC、设备 ID/MAC/serial/key、activated0、分片重组的屏幕码逐字节、salt/verifier 宽度均校验；重新计算 SRP verifier 与认领码匹配，无 raw claim 字符串写入 NVS。

候选 c22b807 的全部 manifest 文件、ZIP、ELF/merged/分区与来源摘要再次核对；16 MiB 擦除态完整镜像先覆盖 merged，最后在 0x9000 覆盖 NVS，各候选段与 NVS 逐字节核对。全部真实码、包、NVS、完整镜像及摘要只保存在云端私有目录，未下载到 Mac/Git。

## USB 与备份现场

Mac 被动 USB 序列号为 `44:BD:8D:79:1F:E8`，VID/PID303a:1001，端口 `/dev/cu.usbmodem21201`，无其他占用。云端 esptool5.6.0 经 localhost RFC2217/SSH 进入 ROM/stub 后，独立报告同 MAC、ESP32-S3 rev0.2、8MB PSRAM、Flash ID20:4018/16MiB、40MHz。安全启动与 Flash 加密均关闭；没有 eFuse 写入。

两份各 16777216 字节完整备份分别读取 503.1/487.8 秒，esptool MD5 传输校验均通过；云端 root/0600，逐字节比较一致，文件及目录均 fsync。分区表 MD5、原启动选择项 CRC 及实际被选 ota_0 的镜像 checksum/SHA256/chip9 均有效，旧认证存在于备份中。未选 ota_1 留有非镜像残余，不作为可启动备份槽；回退资料是整份 Flash，不能只写某个槽。没有擦除或刷写固件。

默认 RTS 复位第一次未退出 stub，40秒无日志；受控同步确认后使用官方 S3 watchdog 完整复位。先启用 WDT、后锁定的最后阶段遇到 USB 复位断连，不能以异常或退出码单独判定结果。relay 按设计停止；被动重新核对同 MAC/VID/PID/location 后显式重开同端口，云端只读客户端保持 DTR/RTS=false，取得54663字节私有串口记录，实际 starting→activating→idle、观察35.3秒、无 panic。没有自动重试刷写，也没有把其他端口当目标。

原固件回到待机后，已关闭 Mac relay、移除 SSH 反向转发；串口无占用，云端42217无监听。最终再核对7份保护摘要、312份源记录/记忆与四服务PID：只有管理已按本批更新，其余语音/旧启动/Caddy保持，ready/memory_ready、active0、factory0/claims0。恢复结论来自实际原固件运行证据，不代表完整备份回写演练通过。

## 手机与切换边界

微信开发者工具中正确测试号工程可生成 58KB 原生预览，但上传按钮/菜单禁用。官方 getwxacodeunlimit 的 check_path=false 已生成真实设备码；check_path=true 的 develop/trial 诊断均返回41030，不能据此证明手机版本可打开，也不能单凭该诊断断言开发版不存在。用户已回复手机预览暂时不方便；手机打开入口、扫码 scene、BLE 权限/Security2/Wi-Fi 和独立确认均待验证。

尚未撤销目标旧认证、把暂存工厂记录应用正式配置、刷写新固件或重新绑定。待手机方便时先核实入口，再按已授权方案切换；不需再次询问是否使用当前微雪。正式上线的一扫进入与开发工具临时预览须分别验收。

## 证据与回退位置

- 后端：`/home/ubuntu/onboarding-migration-20261109/backend/`，含 RED、GREEN、final-race.json、verification-summary.json、tested-source.json、vet/build 与候选。
- USB 测试：`/home/ubuntu/onboarding-migration-20261109/usb/tools/`。
- 云端私有供应：`/home/ubuntu/onboarding-migration-20261109/supply/private/`，包含真实包/码/NVS/完整镜像；禁止公开或复制到 Mac。
- root 私有备份：`/var/backups/family-robot/waveshare-migration-20261109/`，0700，含原配置/认证/身份、完整 Flash、结构与保护元数据和管理部署回退资料。
- `hardware-preflight.json` 记录双份一致，`original-firmware-restart.json`/私有串口文件记录实际旧固件待机，`final-preservation.json` 记录最终保全。RTS首次失败另保留original-firmware-rts-attempt.json，不隐藏未成功的尝试。

当前生产仍是旧设备认证/绑定，因此回退管理发布只需恢复备份记录的旧 current 链接、移除本批100-onboarding-migration.conf并重载/重启管理；不覆盖业务配置。后续一旦应用工厂数据，旧管理二进制不认识新字段，回退须在事务中选择性处理本批工厂/claims并保留并发业务、原绑定和所有记录。完整 Flash 回写回退尚未在实机演练，不能把结构校验写成恢复演练通过。

# 家庭登录更新与远程设备码准备

后续修正：本记录完成时工厂项仍未正式登记，导致远程扫码进入添加步骤返回404。现已选择性预登记同一工厂记录，原码和登录设置保持；本页factory0/待合入项是此前阶段，当前状态以[预登记修正验证](2026-11-09-onboarding-registration-fix.md)为准。

用户明确要求修改现有家庭登录用户名与密码，指定8字节密码；现场无人，本次只完成登录和远程扫码准备，实刷/BLE继续后置。真实密码不记录在仓库。用户另明确允许设备码在电脑/Codex显示，此窄范围授权只涉及PNG展示副本。

## 已实现、测试及部署

`ac4fbf5` 仅把HashPassword/VerifyPassword的共同最低长度从12改为8字节，上限256、随机盐、PBKDF2-SHA256/600000轮、常量时间比较、会话/限流/TLS和权限保持。云端实际RED覆盖8字节摘要/登录失败；GREEN3顶层/10含子用例。完整管理与CLI race81顶层/248含子测试，零失败/跳过，vet/build退出0且无诊断；225份Go/module输入摘要与checkout逐项一致。独立整体代码复审通过。

管理服务单独发布family-login-20261109-r1，最终PID2904568，实际二进制SHA256 `0f26c6733db5c583295d111c0f56f33eca9adcdf59201e8abda6b3665fadfbea`；旧dist-live逐文件摘要保持。用户名变更不增加管理员权限，仍使用原owner-user账号ID、原角色与家庭成员/设备授权。

云端一次性操作通过PTY getpass无回显接收用户指定值，不进命令参数、日志、Git或Mac文件。持现有ConfigStore的同一lock，私有备份及目录同步后原子更改username/password_hash，重启管理清除旧内存会话。脚本复审发现rename后fsync失败可能遗漏回退，已修正为替换前标记变更意图；getpass回显回退告警改为立即失败，备份目录祖先fsync补齐。两项云端故障夹具先在重建不安全版本RED，修正后GREEN，覆盖替换后故障选择性恢复认证/重启和回显故障零变更；专项复审通过。没有新增公网密码管理接口或其他服务。

真实HTTPS新登录、session与families均200，账号有原家庭添加权限；旧用户名401；验证Cookie仅RAM并已退出清除。正式配置逐字段比较只改变原账号的username/password_hash，设备/绑定/正文起点/角色/成员关系均保持。其余6份保护摘要及312份记录/记忆的集合、摘要、大小和mtime保持。语音2556943、旧启动4139310和Caddy2415423保持，ready/memory_ready、active0。

## 设备码展示与现场边界

云端从先前匹配微雪的私有FRC1解码280×280 PNG，CRC/设备ID正确，PNG回读逐像素与待刷固件位图一致。按用户要求，只复制此4451字节PNG到Mac临时私有0700目录/0600文件，并在Codex展示；未公开发布。设备密钥、工厂包、FRC1、NVS、完整备份与固件仍仅云端。

此码用于准备阶段的入口/扫码识别测试。正式factory_devices0、claims0，设备仍是原固件；没有应用暂存工厂记录、撤销旧设备认证、刷机或重绑。手机不在现场时无法完成当前BLE配网。用户手机真实登录、扫码scene解析、BLE/Wi-Fi、独立确认以及实际屏幕码均待验证，不能把云端HTTP200或展示图片记为手机/设备PASS。

## 继续时的账号保全

旧迁移暂存config.json含变更前的登录设置，禁止将它整份覆盖正式配置。云端已生成config-current-login.json保留新登录资料；后续仍应持事务只合入本次工厂记录，保留最新账号、绑定及其他并发业务。切换前保护核对应以本次最终快照为准，账号更名/密码摘要变化属于已授权预期变化。

证据：`/home/ubuntu/family-login-20261109/backend/evidence/`（RED/GREEN/race/source-manifest/vet/build），`/home/ubuntu/family-login-20261109/ops/`（无真实密码源码与故障夹具），root私有`/var/backups/family-robot/family-login-20261109/`（部署、认证前后快照、HTTP及最终保全）。真实码原件仍在先前云端supply/private；本次PNG展示授权不扩展到其他私有产物。

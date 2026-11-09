# 小程序添加设备接入

复用现有 Caddy、管理服务和语音服务。小程序请求源为 `https://robot.songguoxue.com`，设备语音地址为 `wss://robot.songguoxue.com/xiaozhi/v1/`。`api.songguoxue.com` 当前没有业务入口。实际部署与验证见[本批记录](../../docs/verification/2026-11-09-device-onboarding.md)；此前仅 HTTPS 连通的阶段见[原记录](../../docs/verification/2026-11-09-onboarding-https.md)。

## 完整流程

可信操作者在云端供应新 S3 设备，工厂包包含独立密钥和一次性认领码，设备 NVS 与标签码来自同一份包。用户进入小程序，用已有家庭账号登录、扫码、选择有添加权限的家庭；小程序建立官方 Security2 加密 BLE 会话，将认领任务和 Wi-Fi 配置写入设备。设备联网后用自身密钥向自有 HTTPS 独立确认，服务端原子创建实际时间起点的绑定与正文权限，再消耗认领码。只有云端 `confirmed` 才显示成功；Wi-Fi 写入、取得 IP、BLE 完成均不代表绑定成功。

已有账号密码、设备身份和正文起点保留，不导入旧记录。添加权限默认关闭，由可信 CLI 明确授予。当前不提供微信自动建号、手机号授权、转让或聊天小程序。

## 公网范围

| 路径 | 用途 |
| --- | --- |
| `/`、`/healthz` | 提示及连通检查，不能证明设备在线 |
| `/api/family/login`、`logout`、`session`、`families` | 家庭会话与可添加家庭 |
| `/api/family/onboarding`、`/api/family/onboarding/claim_<32hex>` | 创建、查询、取消本人认领任务 |
| `/api/device/activation` | 独立设备认证确认 |
| `/api/device/bootstrap` | 已确认设备获取自有 WSS |
| `/xiaozhi/v1/` | 已有设备或已激活工厂设备的鉴权 WebSocket |

`/api/internal/device-auth` 仅供语音服务回环访问，公网为 404。内部后台、原始记录、Mac 登记/配对、其余家庭 API 和未知路径不代理。Caddy 丢弃客户端 X-Forwarded-For 后重建来源，固定上游 Host/HTTPS；管理服务据此设置 Secure Cookie 并校验写请求来源。

微信原生 wx.request 没有 Origin，且在开发者工具中带 cross-site Fetch Metadata。管理r2仅对可信HTTPS回环代理、无Origin、固定AppID `wxb3f1745f8ed1fa1d` 的严格 `https://servicewechat.com/<AppID>/<devtools或数字版本>/page-frame.html` Referer，放行家庭登录/退出/创建认领的POST与精确认领取消DELETE；其他路径和外部来源不放行。Referer不授予身份，仍须账号、会话及家庭权限；Secure/SameSite Strict Cookie与原浏览器来源检查保持。正式AppID变更时须同步客户端及服务端固定值，重新验证。

## 云端发布

依赖、测试、构建、私有材料与发布均在云端。Mac 只处理代码/Git，可接收无凭据的小程序构建包作为测试客户端。证书、工厂包、二维码、NVS 和固件产物不进 Git，也不下载到 Mac。

1. 核对 DNS、有效证书、实际 systemd ExecStart/drop-in 及当前健康。备份管理配置/发布链接、单元/覆盖配置、Caddy，记录语音环境、产品/设备身份、档案与源记录/记忆摘要。发布前确认语音 ready、memory_ready 且 active_sessions=0。
2. 管理服务发布新二进制并保留既有 dist-live。加入 `--public-origin https://robot.songguoxue.com`，移除配置文件本身的只读 bind，以支持同目录原子替换；记录/档案挂载仍只读，capabilities 仍为空。只给现有 owner 自有家庭添加 can_add_devices，逐字段核对其余配置不变，不在真实配置创建测试工厂设备。
3. 语音候选以实际部署源清单为基线，只接入 onboarding_auth.go、设备入口和可选 CLI 参数，并执行完整回归、vet/build。不能将含其他实验的整个 main 替换正式语音。保留表达式、档案、模型、记忆与所有原参数，新增 `--onboarding-auth http://127.0.0.1:18791/api/internal/device-auth`。已有 MAC 仍只认原密钥，错误旧密钥不走工厂 fallback。
4. 对 Caddy 候选执行 fmt/validate，目标服务就绪后 reload 原服务；保留自动证书续期，不关闭证书校验。
5. 外部检查 TLS/跳转、公开未登录状态及私有路径 404；完整登录/认领/独立激活/重启/WS 准入使用隔离合成配置。正式数据只核对健康和保护摘要，不用真实设备或模型做自动探针。

小程序 request 白名单只需 `https://robot.songguoxue.com`。ESP32 直连 WSS，不要求小程序 socket 白名单；当前页面无上传、下载、UDP/TCP 或预连接调用。白名单已保存、HTTPS 正常或自动测试通过，均不能证明测试号接口权限、备案当前有效或正式发布资格。

## 新设备供应与客户端

供应只在可信云端执行，工厂输出目录为 0700，新包为 0600，不能覆盖已有包：

```sh
umask 077
management provision --config PRIVATE_CONFIG --device-auth PRIVATE_LEGACY_REGISTRY \
  --mac NEW_CANONICAL_MAC --serial NEW_SERIAL --model '微雪 ESP32-S3' \
  --output NEW_PRIVATE_BUNDLE
```

必须提供旧设备注册表以拒绝身份冲突；无旧设备的新环境也须提供明确的空表。供应不等于绑定，不授予家庭添加权限。按 [NVS/固件说明](../../firmware/ONBOARDING.md)生成匹配候选；具备微信接口权限、有效云端凭据和对应上传版本后，用[小程序码工具](../../tools/onboarding_mini_code.md)生成正式码。截图中的旧 AppSecret 不作为输入；未实际生成平台码时不能声称扫码唤起可用。

[小程序工程说明](../../web/miniprogram/README.md)包含云端构建和开发者工具导入方式。固件候选仅适用于新工厂 S3 全量刷写；Waveshare 分区布局已调整，不能向现有设备 app-only/OTA 安装。当前用户设备不会自动刷机、配网、开麦或产生新绑定。

## 回退

本批云端私有备份为 `/var/backups/family-robot/onboarding-20261109-r1`。先恢复原 Caddy 阻断新认领，再恢复原管理发布链接/单元和语音覆盖配置/旧二进制，daemon-reload 并核对健康。保留旧 binary 与原 drop-in。备份时无真实新工厂设备；以后如已产生新绑定，须保留最新权威配置，不能恢复早期 JSON 丢失绑定。仅确认本批没有业务写入时才可还原配置快照。不要清除源记录、记忆、NVS、证书或 DNS。

管理r2来源兼容补丁的单独回退与保全证据在 `/var/backups/family-robot/management-wechat-20261109-r2`；它未改私有配置、Caddy或语音进程。回退该补丁只需恢复此前管理链接/单元，不恢复旧业务配置。

## 当前微雪的原绑定迁移

已有 legacy 身份的微雪必须使用迁移命令，普通 `provision` 仍拒绝旧注册表中的 MAC。先部署支持 `migration_binding_id` 的管理候选，再仅在云端、完成可回退完整 Flash 备份后执行；参数中的设备 ID 和绑定 ID 必须来自当前管理配置，MAC、旧认证摘要和只读 voiceidentity 身份必须一致：

```sh
management migrate-legacy --config PRIVATE_CONFIG \
  --device-id EXISTING_DEVICE_ID --binding-id ORIGINAL_ACTIVE_BINDING_ID \
  --mac VERIFIED_MAC --serial UNIQUE_SERIAL --model '微雪 ESP32-S3' \
  --device-auth PRIVATE_LEGACY_REGISTRY --identities PRIVATE_VOICE_IDENTITIES \
  --output NEW_PRIVATE_BUNDLE
```

旧注册表和身份文件要求普通私有文件（0600），拒绝符号链接、超限、重复 JSON 字段或未知字段。输出目录须为 0700，新私有包为 0600 且不能覆盖。命令仅输出状态；包仍为原有九字段，新密钥与认领码不能打印、复制到 Mac 或入 Git。新包持久化后才原子追加工厂认证；重复迁移、错误 ID/绑定/型号、未生效或已结束的原绑定均失败，不撤旧认证、不修改原身份文件或家庭权限。配置提交结果不明时保留包并人工核对，勿反复供应。

迁移不自动绑定。小程序扫码只允许原家庭中同时有添加设备权限和原设备访问权的账号。取消/过期后必须重新认领，确认及重试会再次核对当前权限和原绑定；独立设备确认前，新密钥不准入语音。确认后复用原设备资料、设备 ID、绑定 ID 和原绑定起点，不增加设备、绑定或正文权限。原有记录路径和绑定前访问限制保持。准备完成后撤销目标旧认证、安装与完整读回及手机扫码/BLE 验收须按迁移计划另行执行，CLI 成功不代表设备或手机验收通过。

回退旧管理二进制前须处理其不认识的迁移字段；在锁定事务中仅移除本次新增工厂/认领数据并保留并发变化，不整份覆盖管理配置。原设备、原绑定及历史记录继续保留。

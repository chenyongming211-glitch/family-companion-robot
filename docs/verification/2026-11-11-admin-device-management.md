# 内部后台真实设备控制与记忆管理验证

2026-11-11已发布。用户批准按原计划补齐内部后台的真实在线状态、音量/亮度、近期记忆清除与操作回执；本批复用家庭小程序的同一管理服务、命令队列和Voice记忆Store，未扩展搜索、邀请、留存/删除或新智能体能力。

## 实现与审查

- Vue功能aa7eacb、修正bef2dae；Go功能91bee7078f515c938deefa09ae23d68529401f69，均直接main提交推送，无分支/worktree。
- GET/PUT `/api/admin/device-runtime/{deviceId}`、POST `/api/admin/memory-clear/{deviceId}`复用family处理器。admin独立会话与当前InternalAdmin、明确设备授权均须成立；控制还需当前家庭CanAddDevices，清除还需正文权限。审计记录真实scope。账户/授权配置没有改变。
- runtime命令保存不导出的scope，设备取指令/回执按当前scope权限核验；请求等待runtimeMu期间撤销角色/设备/管理权限的旧快照问题，已用确定性屏障复现RED并补锁后纯snapshot/权限校验，无Server.mu/account锁倒置。
- 私有MemoryClearClaim增加可选scope，省略兼容旧六键family；显式scope仅family/admin，空、null、大小写别名、重复或额外键拒绝。Voice仍在同一config.lock内新核验权限/绑定，持锁到Save receipt及读空，Store唯一写入不变。先Voice后管理升级、先管理后Voice回退保持兼容。
- 网页admin/family共用RuntimeControls与集中API。20秒心跳在线，0..100整10/11圆点，松手一次提交；45秒命令、前端50秒观察。完整匹配设备/boot/命令/字段/目标/actual才提示生效，未知不重试。已有queued/sent明确等待并禁止新提交。
- 近期记忆需本次正文读取成功及管理权限提示，一次确认、防重，匹配confirmed再重读；聊天保留。隐藏/卸载/切设备/路由/退出/401/账号变化清敏感状态、取消观察并隔离迟到回调。Cookie/正文不新增storage缓存。昵称/位置网页编辑、成员邀请和删除/留存仍未接入。

独立任务及整体规格/质量审查PASS。两个P2均已修复：Vue刷新会解除尚未结束的清除保护，修正为pending时拒绝load/禁用刷新，并保留真实路由切换清理；运维helper只盘点/etc目录、遗漏有效systemd DropInPaths，修正为完整路径/摘要及reload前后加载集合核对，外部未知drop阻止重启。没有遗留可操作问题。

## 云端测试与实际来源

所有依赖、运行测试和构建在云端；Mac仅代码/Git与浏览器客户端。

| 范围 | 结果与边界 |
| --- | --- |
| Go main相关完整race | 717通过（298顶层），2原有可选跳过，零失败/竞争；management387.590秒、realtimelive36.479秒 |
| Go vet/两个命令build | 最终源码通过；192项公开输入摘要核对，文本清单SHA face866cf803692f871fc11bf46275c401e8508ae9eee594b071c31c6efd09de |
| Vue完整行为/组件测试 | 53/53；typecheck、mock/live两构建通过；新刷新/清除竞态mounted RED→GREEN，focused5/5 |
| Vue构建来源 | 35项Mac/云端相同，构建时文本清单SHA 2d94765f20c5dcef1ab4eba1ee3db50598b02e55dbd9bffe9beb6b56719e4161；后续仅交付文档更新不改该构建来源 |
| 实际候选race | management按Admin/Runtime/Memory/Authority相关130通过（32顶层），完整Voice/Store/两cmd449通过（207顶层），合计579/239，2原有可选跳过；不是重复全跑management全部用例 |
| 实际候选vet/build | 相关5包vet、两trimpath命令build通过；178项源码及3公开fixture检查前后不变 |
| 最终运维脚本 | 61 helper＋18原门控共79项在填入真实pins后的同一字节版本通过，7.133秒；systemd/防火墙使用合成fixture，无真实清除 |

Go初次完整测试因上传遗漏公开config/product-identity.json而失败，保留日志并补齐fixture；另一次完整测试因新复现runtime等待竞态而停止其已知测试子进程，再以最终修正源码全跑通过。权限负例沿用setpriv去dac_override/dac_read_search、保留UID0及组0/1001；未弱化产品权限或测试。

没有整包部署main：实际两服务共同176项baseline canonical SHA `9f7bd2fe9991385460bbb77253d62beeb3ef133d9f4b21bb5088b36dbf1f0413`；仅应用本批4修改/2新增Go测试，patch SHA `b897d4c68e5f2dec2556c3f15498c6f2bbbe35e3b47e6b4decbda9d19c8ca7b2`。反向patch逐项恢复全部176基线，保留真实旧语音差异；新178项canonical SHA `e870a511f501cfd7240e9a45e103c61bed6d3a4fd20fa7568e11419f7146deb4`。两服务同源。

## 浏览器合成流程

云端合成API提供明确标为“合成测试机器人”的两设备、虚构正文、心跳和回执，Mac Ego Browser仅作客户端。最终main-CmM3MqOg.js上核对：音量70→60、亮度60→70各1次PUT并确认；来源跳到合法业务chat与turn；取消清除0次POST；保持清除响应3秒时刷新被禁用、重复操作仅1次POST，确认后记忆为空而聊天档案仍1条。离线禁控、只读隐藏清除、正文403清内容、错误设备回执不报成功、busy提示和家庭网页共享控制均通过。375px与1440px布局及11圆点已检查，截图为合成界面。

Ego高层click临时触发hidden/visible，使隐私清理正确地卸载目标；诊断后使用其文档允许的DOM/CDP操作及合成confirm回调，不削弱产品清理。原生键盘滑块操作通过；不将合成确认写为真人原生弹窗/手机操作或设备听感验证。中间浏览器HTML缓存曾指旧资产，已核对云端/HTTP真实资产并禁用本测试页面缓存，以最终CmM3MqOg重跑；正式管理服务实际index验证为no-store。TaskSpace已结束，合成服务与仅为本次增加的18794 SSH转发均关闭。

## 正式发布及独立核对

两服务发布目录均为admin-control-20261111-r1：

| 服务 | 实际PID | 实际SHA256 |
| --- | --- | --- |
| Voice | 1161080 | eab47a77110548a09d4dc0340c6a52660d9a7a926c587d220640ff95672e3519 |
| 管理 | 1161165 | 1e1d778bfa531bb039d8029135ee22672149a91818f45632199e02c7da6b9cd1 |

管理新静态为release/web，4项canonical SHA `8da2c13be24160dcc084ff3907b3f7c74e573d12c14fe61d6a994e3cb3d99d3b`；JS SHA `bf4c6350556a8312c77bdbc4bf9dd7407164d7003476d24710b9ed341d151b80`。旧current链接及旧dist-live保持；管理argv只替换exe/单一static值，Voice只替换exe，其余原argv/env和RW挂载保持。

已用既有IPv4/6 NEW SYN门控、独占健康探针和内核连接清单保护空闲切换；私有陌生admin身份403就绪后才切管理。最终已填pins的helper SHA `bf111a9c9cf4e1fdadce16429f2d905a8cc419e73453f1178cfb2bcdc541968f`。本次部署一次成功，无Caddy变更或reload。

上线后独立检查：ready/memory_ready=true、active0、max_sessions2、三项diagnostics=false；私有目录root0700/socket0600，GET405/Allow POST，scope=admin且配置不存在的测试身份POST403，响应早于两秒deadline；未尝试实际Store清除。内部admin未登录401、公网admin新路由404，原family公开路由未登录401，Voice公共handler私有clear404。

实际二进制/source/web SHA与参数/环境通过；HTTP提供的新index SHA相符且Cache-Control=no-store。原10保护文件、全部记录JSON/JSONL/.memory、原配置/偏好/绑定、旧static/current链接及另一Go PID4139310/Caddy PID2415423保持。Caddy内容SHA dd710587a616186a5fde46f38f30d281b99eafed23f4a3b821f851f166e79692及uid/gid/mode/mtime/ctime均不变。Voice原3项ReadWritePaths与config.lock权限保持。主机iptables/ip6tables -S摘要与切换前一致，临时发布规则0。

切换前后以候选Reader在云端RAM作admin授权只读核对，原24条记忆全部找到授权业务聊天和轮次，来源缺失0；原文/凭据未输出或复制Mac。本批未对真实设备设置或清除、未开USB/刷固件/触碰退货M5，统一S3 600ms及现有语义控制保持。

云端来源/测试/证明位于`/home/ubuntu/admin-device-management-20261111`；私有事务/原参数环境仅`/var/backups/family-robot/admin-device-management-20261111`，不得复制到Mac或回写旧配置。实际网页入口仍是现有SSH转发18791的index.html；管理重启需重新登录。现场实际操作仍待用户方便时验证，本批不以预览或现场测试阻塞开发，也不补写现场PASS。

实施依据：[方案](../superpowers/specs/2026-11-11-admin-device-management-design.md)、[计划](../superpowers/plans/2026-11-11-admin-device-management.md)。

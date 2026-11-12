# 网页设备昵称和放置位置

2026-11-12。用户确认补齐家庭网页、内部后台编辑入口和后台权限适配。代码、云端验证、隔离浏览器联调和管理/网页发布完成；生产只读验收通过，网页实现不等于手机现场验证。

## 实现和架构审查

Go复用 settings.go 的规范化、版本和 ConfigStore 事务，仅传入已验证的family/admin scope。读取要求当前绑定和明确设备授权，后台另需InternalAdmin；写入另需CanAddDevices。取得写锁后重新核对会话、权限、绑定及版本，审计记录真实scope。昵称1至12个Unicode码点，位置可空、最多24个。只写display_name/location，语音称呼仍由原voice profile管理。

Vue复用一份DeviceSettings表单和API，controller仅持RAM草稿。一次PUT后GET，核对设备、昵称、位置及版本才显示保存成功。冲突、失败或30秒无确认均要求重新读取，不自动重发；隐藏、退出、401、切换账号/设备/作用域/路由使旧结果失效。两次审查/联调发现并修复连续保存旧刷新覆盖标签、冲突后重新读取未同步标题/设备列表；均有先失败再通过的真实编译组件回归。

最终规格、代码和架构独立审查通过，在审查范围内无剩余Critical/Important。Go持有规则和事务，API检查契约，controller管理异步状态，组件管理展示和生命周期；无新依赖、产品mock、持久化草稿或无关重构。源码b69389a、f0f212f、44e3078；冻结230项输入逐字节匹配44e3078da3b4c51a20701dee48919b959a94ef87。并发音乐固件及原audio-capture/M01工作不属于本批。

## 云端验证

所有运行测试、依赖和构建在云端；Mac仅代码/Git和浏览器测试。

| 对象 | 完成检查 |
| --- | --- |
| 最终网页 | 82/82测试、类型检查、生产构建通过，其中22项真实编译Vue组件用例 |
| public Go | management完整功能124顶层/348含子用例、相关race21/70、vet/build通过，零skip |
| 实际来源候选 | management完整功能122/346、新AdminSettings race4/16、vet/build通过，零skip |
| 发布操作器 | 40项云端隔离假环境测试；另独立检查真实helper、systemd及10秒优雅退出来源 |
| 只读验收helper | 最终11项云端隔离测试；只允许登录、GET及注销自己创建的会话 |

完整management race未完成，不计通过：首次10分钟超时，之后20分钟尝试因2CPU与同期固件编译竞争而显式停止143，保留失败/中断日志。对仅scope适配、未新增共享状态或锁的Go改动，最终采用相关race加完整功能；独立后端和最终审查未发现还需补查的具体并发风险。最初非root触发既有审计归属夹具失败，随后UID0去掉DAC绕过能力真实执行所有权/chmod拒绝用例，未放松产品断言。

实际管理来源从已部署c8c2edcd基线只叠4个任务路径，183到184项全部输入核对，保留旧onboarding/Voice来源差异和3个非Go支持文件，没有用public整包替换实际来源。

- 实际Go source digest：155defefbbeb0b1340af4d393ff0ae5fb0e44f1a9fd95103d423373f256c73cd。
- 二进制11,998,976B，SHA256：3f07130327199e2ae05fb4397a30418073482177a63f00fc5fa26e7b5ced011b。
- 网页38输入清单文件SHA：9c4d0b5dc93b0ccd50031bb464ba63d45ea6ad5d08e270c2a7beb6ac817689d5；4产物清单文件SHA：3d9909240c7dfac7f4e8010e1b0a4fc49a6453739ec3adc121aa259a80862ed0。
- 网页canonical map digest：8d9627cd91fe945c9a24de0cf1a5af22d6d76372fa06cf1309a6f3ebb59d9e40；JS main-D8jiwrLl.js，SHA：9f8d0e77ff0b4a5390206b9202ca02234331f3aaac368c97829182ed0c47e93e。

完整来源、测试和私有运行证据位于云端/home/ubuntu/web-device-settings-20261112；无凭据、Cookie、实际正文、原始音频或设备备份复制到Mac/Git。

## 最终浏览器联调

隔离实际候选二进制、最终真实网页、合成账号和两个设备，专用hostname/18797，未使用生产配置/正文。

- 家庭保存后后台读取一致；后台保存后家庭刷新昵称、位置和版本一致。
- 并发修改409，旧草稿不能继续保存；重新读取后表单、标题和选择列表一致。
- 只读账号输入/保存禁用且有说明，仍能重新读取；直接PUT403，存储版本保持。
- 仅家庭Cookie访问admin401；有后台角色但无设备授权时GET/PUT403，不开放编辑表单。
- 两端375/1440宽度无横溢，输入/按钮44px；截图视觉检查保持现有风格。
- 超时、网络失败、迟到结果及隐私失效由controller/编译组件自动化验证，未额外在浏览器注入故障，不冒充浏览器覆盖。

TaskSpace12已finish，合成进程停止，只有新增18797转发取消，原18791/SSH保留。高层fill触发浏览器可见性清理时，改用合成页面DOM输入/提交，未关闭产品隐私逻辑。

## 磁盘清理

用户报告100%后先暂停发布，实测仅约124MiB可用。核对无编译/安装进程、无打开引用后，删除Go构建缓存、pip/npm/ESP组件下载缓存、5个超过24小时且无硬链接的Snap下载缓存，以及与保留候选字节一致的重复二进制。共释放约6.5GiB，降至90%、可用约6.7GiB，本任务目录约39MiB；源码、日志、证明、保留候选、实际安装包、凭据、设备备份和记录保留。原管理/Voice PID与摘要保持，健康检查通过。此前另已清理本任务81MB依赖副本和14MB已结束Go临时目录。

## 生产发布与边界

发布前已核对管理1401849/b49、诊断Voice1608608/261a87d、quiet及保护状态。真实只读preflight核对家庭设置200、原后台设置404、两端聊天/24条记忆/runtime及后台审计200，两临时会话已注销；这是旧版本基线，不把404计为新能力通过。此时runtime为offline、音量/亮度未知，没有声称微雪在线或实机控制通过。最终管理已切换至web-device-settings-20261112-r1，PID2273712/SHA3f071303；仅更换可执行文件和static参数，原release保留。本批deployment保护前后相同，验收后再次核对相同。四个实际HTTP产物200、SHA与最终pins一致、Cache-Control no-store；Mac原18791转发两入口200。

授权只读后验收：family/admin设置均200、DTO及canEdit有效，两端昵称/位置/version完全一致；聊天、24条记忆、runtime及后台审计200。最终helper558e实际执行通过，两临时会话已注销，没有生产PUT、控制或清除。预检runtime为offline，管理重启后为unknown、音量/亮度未知；未把接口验收写成微雪在线或现场控制通过。

最终pins SHA9a161812、不可变review binding c08c1f99、root授权fd22dc05分别保留；deployment证明SHA d1b8258e、真实只读accept证明 ddbe22a7、production-static-proof/production-final-proof位于本批云端目录。新私有事务位于/var/backups/family-robot/web-device-settings-20261112，owned drop为180-web-device-settings-20261112.conf；无自动重试、自动回滚或用户数据恢复，管理重启后旧RAM登录需重登。

操作器只切换管理二进制和静态argv，保留原release、配置/身份/绑定/profile/档案/记忆、旧static/current、审计前缀、权限/socket/锁、Voice及其他服务、Caddy和本次前后防火墙。最终pins与源码/构建/审查/浏览器证据另行不可变绑定；失败保留证据，无自动重试、自动回滚或旧数据恢复。

原admin-audit中断事务和原始防火墙差异仍独立未关闭，strict_original_transaction_acceptance=false。本批不改微雪固件、语音称呼、音量/亮度，不为测试改生产昵称/位置或清除记忆。生产PUT和手机三端刷新未执行，保留为现场验证边界；P3其余成员权限、留存/删除及P4/P5未完成。

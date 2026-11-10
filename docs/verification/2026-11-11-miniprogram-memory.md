# 小程序近期记忆管理验证（已发布，手机交互待验）

用户批准查看最近7天/最多24轮实际续聊摘录、查看来源聊天、管理员清除且服务端确认后再提示完成；聊天档案保留。实现只扩展现有Go管理/语音服务和原生小程序，无新服务、依赖、模型调用或固件改动。

## 实现与代码审查

- 后端`e54f1e89d39eddb68adc44033700da7d9eaba007`：现有memories数组增加可选chatId/turnId，按授权后的session+turn映射业务聊天；新增家庭POST memory-clear/{deviceId}，严格confirm:true与新权限/绑定核验、先持久审计。
- Voice仍独占Store；私有root0700/0600 Unix HTTP固定路由。任意设备lease（含握手/收尾）返回busy，clear gate阻止同设备新lease；config flock新核验actor/正文/管理员/绑定，持锁到实际Save receipt完成，独立2秒FIFO读回确认空；HTTP截止不提前释放写入保护，不自动重试。原语音清除保留floor/base/control重置，存储核验复用。
- 前端`217b00cb568d3bba5b44f1d5db3345cf973d50b9`：设备详情“近期记忆”、真实摘录、来源聊天、一次确认、只读成员隐藏清除；busy/未知不报成功。退出/后台/401/账号变化清正文并隔离迟到modal/HTTP。
- 独立组件与最终源码审查均PASS。曾发现审计/配置失败错误码为通用source_unavailable，已补RED后统一为memory_clear_unconfirmed，保留400/403/404；无未解决重要代码问题。

## 云端测试

小程序先19项实际处理器/原生绑定RED，再补cookie变化/未知回执RED；最终170/170（148原有+22新增），零fail/skip，typecheck/build/native均通过。63份输入Mac/云端相同，清单SHA256`e510b2a92430f9ca4b52c03695805f7b404c25a297a9b1d8090a969083ec93cd`；26份dist清单SHA256`eb777d0a744db1d73cf294b40685ab6ea6435da98957f9c402b6894791a34247`，Mac客户端逐份核对。官方微信CLI实际preview成功213360B，不能据此补记手机触摸或真实清除PASS。

后端先新路由/来源行为RED，核心及新增竞态GREEN；完整main相关5包race317顶层/730含子测、2项原有可选跳过、零失败/竞争，vet和两命令build通过。原有onboarding权限失败测试在UID0/CAP_DAC_OVERRIDE下错误返回200，已在未改198ab0c隔离基线复现；仅测试子进程setpriv去dac_override/dac_read_search、保留UID0与组0/1001后通过，未为此改产品或放宽测试。最终管理全包276.969秒，另4包显式-count=1无缓存通过；保持完整失败日志。

## 真实运行来源的最小移植

当前实际Voice基线135项，与main还有7份未改语音源码不同；不能整包部署main。由实际135项+已核对且与198ab0c一致的管理35项+两份b1至198未变conversation依赖组成172项，再仅应用提交198..e54的Go feature patch（49605B/SHA256`1a353b57c9198aac280c19d6672947bf8d3c09e80cd61afc2cd257bb81364a80`），check/apply成功，最终176项。反向patch逐字节恢复172基线，保留实际旧语音实现；两服务使用同一176项来源，canonical map SHA256`9f7bd2fe9991385460bbb77253d62beeb3ef133d9f4b21bb5088b36dbf1f0413`。历史2份._旁文件保留而Go忽略，3份公开测试fixture另记摘要。

实际候选完整Voice/Store/两个cmd及受影响管理memory/chats/reader/authority/settings测试，247顶层/587含子测、2项原有可选跳过、零失败/竞争；并非声明所有管理包再全量跑一遍。vet5包和两trimpath构建通过，全部176项检查前后不变。Voice二进制SHA256`b9a4e20ce9109631999affc87b69d5e6f49431c7e0cdb6c91bec39b823fc4545`；management `00727d997822e13db2424d8fdab095a2bb818076dc5ba8721fe71fef6a4510ef`。

旧management release没有manifest，35项直接源码与旧source/Git基线全部相同；相同路径/工具chain rebuild为0635而非现行7e7e，未建立旧完整依赖/产物复现。保留失败并明确只采用known-direct-packages部分基线+实际旧二进制身份，不伪造旧manifest或声称全依赖复现。新候选来源/增量及实际构建均重新锁定。

## 只读真实数据核对

用候选Reader在云端内存读取现有配置与记录，24条近期记忆全部找到授权业务聊天及精确轮次；0条来源缺失。只输出计数/授权结果，没有复制正文或凭据到Mac，没有清除真实记忆。正式切换后逐项摘要与原始保护快照一致，真实记忆未被测试清除。

## 发布边界与准备

准备新Voice recent-memory-20261111-r1与management memory-pages-20261111-r1，保持原argv仅追加私有socket及Voice management-config；当前管理--config路径已核对与新guard相同。Voice ProtectSystem=strict，需仅追加socket目录和现有config.json.lock的ReadWritePaths，保留原记录目录RW，配置JSON/目录不新增写权。实际lock为root普通0600文件；Voice无AF_UNIX排除，管理已有AF_UNIX。

复用已有IPv4/6 NEW SYN门控与专属探针、内核连接清单，覆盖两服务重启、Caddy及回滚；不切断旧连接，不回写旧数据/配置。旧保护11项包含Caddy，故10项不可变保护单独保留，Caddy另以原/候选SHA管理明确变更。新Caddy只加GET /api/family/memories和POST /api/family/memory-clear/dev_[a-f0-9]{32}；复用原管理handler，adapt后移除新增两route、规范group ID则与原JSON完全一致。旧SHA e6eb3dc31a4576e747c2fd00db2cfdf1e7a056405d875b41de81343f04c9c1b6，候选dd710587a616186a5fde46f38f30d281b99eafed23f4a3b821f851f166e79692。

最终私有listener只用GET405和不存在的测试身份POST403核对就绪/配置锁权限；不对真实家庭/设备执行清除。私有403还要求单调时长<2秒且响应早于claim截止，避免过期403被误作配置锁就绪证明；迟到反例已RED→GREEN。

实施：[方案](../superpowers/specs/2026-11-11-miniprogram-memory-design.md)、[计划](../superpowers/plans/2026-11-11-miniprogram-memory.md)。


## 正式发布与故障恢复

已发布Voice `recent-memory-20261111-r1`，PID368937/二进制SHA `b9a4e20ce9109631999affc87b69d5e6f49431c7e0cdb6c91bec39b823fc4545`；管理 `memory-pages-20261111-r1`，PID368956/SHA `00727d997822e13db2424d8fdab095a2bb818076dc5ba8721fe71fef6a4510ef`。源码提交e54f1e8，文档后续提交不改变此构建来源。两服务实际exe/hash/argv/env独立复核通过，保留原参数仅加本批明确flags；原管理static与current链接保持。

第一次切换两服务和私有检查成功，但Caddy reload因配置读取权限失败。根因是运维helper的umask077把atomic请求的0644削为0600，Caddy用户不能读取；回滚恢复旧配置字节后因同一权限问题停在reload，未把它写成完整回滚成功。两个已知新服务健康、Caddy原进程继续旧配置，finally门控已清理，未恢复或改写用户数据。新增显式模式测试复现RED；写入前fchmod到请求模式修正，私有默认0600保留，最终辅助42项+gate18项共60项通过。

采用独立审查认可的受控继续发布：从私有事务记录重建旧基线，先核对两个明确新exe/hash/argv/env、owned drop摘要、全部来源/产物和原配置/档案/static，取得新IPv4/6门控并再次quiet/保护核验；私有检查后重新publish/verify_new，失败仍走正常门控回滚，finally清理。Caddy原字节→候选字节且实际0644加载成功；健康的新Voice/管理未再重启，PID仍368937/368956。保留第一次失败及恢复记录，不覆盖失败证据。

独立上线后检查：

- ready/memory_ready=true、active_sessions=0、max_sessions=2、三项diagnostics均false；私有root目录0700/socket0600，GET405/Allow POST，当前配置不存在的合成身份POST403且响应未过截止；没有对真实设备发清除。
- Voice有效ReadWritePaths恰为原records目录加socket目录、config.json.lock；配置JSON/目录未扩写权，配置lock仍root普通0600。
- 公网实际HTTPS记忆GET和合法形状清除POST未登录401；非法clear、私有/memory/clear与未公开admin路径404。未凭空补记手机登录后的真实清除成绩。
- 原10项不可变保护文件、全部JSON/JSONL和.memory、原网页static全集、另一个Go PID4139310、Caddy PID2415423及management current链接保持；Caddy内容仅预审两路由增量，SHA dd710587a616186a5fde46f38f30d281b99eafed23f4a3b821f851f166e79692。
- 两次门控均退出；iptables/ip6tables的完整-S摘要与发布前一致，临时family-voice-deploy规则0。保留统一S3 600ms、原语义控制、当前偏好和家庭/设备绑定，未开USB/刷固件/触碰退货M5。

云端私有证据：`/home/ubuntu/miniprogram-memory-20261111/{final-production-proof.json,initial-deploy-failure.json,before-firewall.json}`；事务和基线仅`/var/backups/family-robot/miniprogram-memory-20261111`。不得复制baseline中的argv/env、配置或原始数据到Mac。产品/最终运维审查无剩余重要问题。

## 使用与验收边界

新版小程序：登录家庭账号→设备详情→近期记忆，可查看7天/最多24轮摘录、查看来源聊天；管理员可点清除近期记忆并作一次系统确认。机器人对话中返回busy，结束对话后再操作；未知不报完成、不自动重试。清除只影响续聊上下文，聊天档案继续保留，后续新对话可产生新记忆。

云端合成双服务/Unix完整清除、真实写回读、聊天保留和权限/竞态负例已通过；正式环境只读和拒绝路径已验证。手机页面、来源跳转和管理员实际清除尚未现场操作，不宣称真机体验或真实清除验收PASS。管理重启清空原RAM登录，会需要重新登录；未变更账号/密码。

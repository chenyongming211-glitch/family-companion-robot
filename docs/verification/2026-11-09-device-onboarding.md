# 2026-11-09 小程序与设备认领

本批按用户已确认的小程序＋蓝牙入口实施，覆盖原生页面、注册认领接口、独立设备激活和新工厂 ESP32-S3 候选。代码直接 main；所有依赖安装、测试和构建在云端，Mac 仅代码/Git与接收小程序测试客户端。现有设备未刷机、配网或开麦，无真实新绑定和模型调用。

## 最新：扁平主题与微信原生预览

用户提供两张参考图后，`6af25aa` 将现有添加流程改为蓝紫浅色、白卡片、扁平机器人/四步骤图标及局部半透明叠层。只改模板/样式/导航背景；全部事件、输入、阶段条件及仅云端confirmed成功的规则保持，未增加无业务支持的底部导航。

真实开发者工具检查发现并修正三类问题：`3cab149` 用原生App回调receiver处理首次onShow早于getApp的顺序，并兼容固定本应用Referer的原生wx.request；`ba6e620` 保留字面量CommonJS require以防Security2被依赖扫描过滤，将模块降为ES2018语法、等价BigInt构造器表达式以兼容原生打包。BigInt能力检查、延迟加载、认证和协议均保持。三个范围源码复审通过，无遗留阻断发现。

最新同源云端结果：70/70测试、0skip（1.620s）、tsc、构建及真实WXML阶段可见性检查通过；构建后的Security2也执行官方完整cmd0/cmd1及双向密文向量。协议源码专项9项通过。后端完整race（management113.653s、CLI8.767s）、vet/build通过；固定AppID来源边界25种输入及错误/正确合成家庭登录均覆盖。9份修改源文件与最终云端校验源摘要匹配，7份Mac预览内容与最终云端dist摘要一致。当前前端/后端新增提交不冒称已有4a341e3的CI结果。

微信开发者工具 Stable 2.01.2510280、基础库3.10.3实际观察：冷启动及蓝紫扁平首屏正常，iPhone12/13(Pro)模拟器无首屏重叠；调试器Errors0、Warnings2（SDK系统信息提示）。合成错误家庭账号通过真实自有HTTPS返回401 invalid_login，页面显示“账号或密码不正确”。最终压缩预览成功，代码包58KB，仅过滤project.private.config.json 1项，Security2已入包；合法域名校验始终开启。已向用户显示预览码（该次有效至11/09 16:15），可在工具重新预览。预览打开开发版，不代表工厂码/设备认领成功。375px、后续业务阶段的原生视觉与Android/iOS实际BLE仍待验收。

管理服务已单独发布 `onboarding-20261109-r2`，PID2645283，二进制SHA256 `93361485de143608051ea0ea9e077765f6a0aa5c2e22cbc55bf8e047146256e1`。只增加上述严格来源兼容，账号/绑定/私有配置摘要不变；5份保护文件保持，voice仍r1/PID2556943、ready/memory_ready=true、active0，未重启语音。证据 `/var/backups/family-robot/management-wechat-20261109-r2/verification.json`。r2后再次公网42项、TLS1.2/1.3和308全部通过。以下r1段落是首次发布记录。

后续核对：实现及发布记录提交 `4a341e3102a9f64f96a2dc9d5bdabf68f90a3a92` 的 GitHub Actions 已全部成功，[实验工作流](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/42570044086)四个 ESP32-S3 job 与[stock工作流](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/42570044069)一个 job 均为 success。常规 CI 和本批新工厂候选的云端两板构建是分别验证的，均不代表手机/设备现场通过。

## 实现与边界

- 复用家庭账号和 Go 管理服务。默认无添加权限，可信操作者可单独授予。配置每请求读取快照，跨进程 flock、锁内重读和原子 fsync 写；供应私有包先持久化再提交权威配置。
- 一次性认领任务限本人及有权限家庭，设备必须用独立工厂密钥确认才创建绑定/正文权限。绑定从实际服务端时刻开始，旧正文不导入；重复确认按原结果返回，重启可恢复。
- 原生 TypeScript/WXML/WXSS 页面：家庭登录、扫码/启动 scene、选家庭、发现设备、加密会话、Wi-Fi、等待云端确认。Cookie、认领码和 Wi-Fi 密码仅在 RAM；取消、后台和过期按操作/会话代次清理，防止旧回调影响新任务。
- 锁定官方 ESP-IDF6.1/network_provisioning1.3.2 Security2 patch1。官方 Python 互通向量验证 SRP3072/SHA512、双向 AES-GCM；20字节 GATT 使用配套 framing，不降级明文。该证据不能代替设备 C 算法与真实无线互通。
- 新工厂 Waveshare/M5 固件使用自有 HTTPS/WSS、证书校验、校时和独立激活。仅新工厂全量刷写，Waveshare 分区已调整；不是现有设备 OTA/app-only 升级。
- 可信小程序码工具只在云端读取 0600 工厂包/微信配置，固定官方 HTTPS、页面和 scene；无真实微信调用或凭据使用。

## 自动验证

| 范围 | 已观察结果 | 限制 |
| --- | --- | --- |
| 管理权威源/HTTP/CLI | Task1 67 主测试、178含子用例 PASS；最终合并源码完整 race（管理300.664s、CLI13.069s）、vet/build PASS，专项源码复审通过 | 全为合成身份；不代表 owner 真登录或实机绑定 |
| 小程序初次合并源码 | 云端清洁 npm ci；初次68/68测试、0skip；tsc、打包及独立 App/page 构建产物校验通过；audit 0漏洞；最新70项及原生结果见上节 | 无手机权限与扫描时序验收 |
| Security2 | 9协议测试；固定官方 Python 原始源码完整握手与双向密文交叉校验通过 | 非手机与 S3 真无线握手；前导零 A 的额外向量为后续覆盖项 |
| BLE framing | 实际 C host 测试4项通过，430字节请求/响应、边界/超时/重放偏移/断开/用户暂停；实际 S3 编译单元通过 | 无真实 GATT/RF；32位日志时钟49.7天回绕时在途请求可能误超时，180秒初始配网窗口下列为非阻断项 |
| 小程序码工具 | 16测试 PASS，源码/取消清理复审通过 | 无微信接口权限、token 或正式码生成验收 |
| 管理→语音 | 真实临时配置、真实登录/认领/独立激活/持久化恢复、固定自有 bootstrap及WS准入，最终专项race20.275s通过 | 不发送 hello；无模型、音频、真实记录或播放 |
| Caddy 代理 | 实际候选配置＋隔离管理实例31项检查通过：Secure Cookie、来源、XFF限流、私有404、认领取消/激活/bootstrap | 代理夹具使用回环 HTTP；有效公网 TLS 已在上一批单独验证 |

选择性语音候选来自当前实际部署清单的134份 Go/mod/sum 文件，全部摘要先匹配，再只应用新认证桥；没有使用含其他实验的整包 main。缺失的公共测试脚本/产品模板从同一基线补齐，单列摘要。第一次全量回归发现既有 PCM 测试助手没有 join 后台 OutputASR 审计写入，清理临时目录出现竞态；复现后仅修正测试资源生命周期，不改正式语音算法。最终 PCM race 连跑30次通过16.904s，完整候选 `go test -race ./...` 通过（realtimelive28.366s），完整 vet 和独立构建通过。

整体源码复审及最终修复复审通过。固件曾发现校时迟到后不能恢复，以及 worker/断网通知竞态；2aab4f4 将通知统一在默认事件循环串行，并用实际 C++ 函数确定性回归覆盖迟到校时、过期事件及发送失败。最终136/136固件 host 检查通过、0skip（188.491s）。同源 M5/Waveshare 最终构建、merge、ZIP、实际二进制分区表/CSV、flashargs、16MiB边界和有效配置全部通过，编译日志零 compiler warnings。M5 app3,407,072字节/OTA4,128,768字节、assets5,376,385/8,388,608；微雪app3,393,488/OTA3,604,480、assets5,376,385/6,291,456。两个 Minor 后续项为时钟回绕误超时及 SRP 前导零 A 的附加向量，无已证实算法错误。

## 云端发布记录

首次发布 `onboarding-20261109-r1`。管理新二进制保留原 dist-live；只给 owner-user 在 family-home 新增 can_add_devices=true，完整配置深比较确认其余账号/口令摘要、设备、绑定时间和正文权限不变，无真实 factory_devices/新绑定。语音仅发布从实际旧基线派生的认证桥，原134份源只修改cmd入口、设备入口及PCM测试清理，新增2份认证源码/测试；五个改动/新增文件逐份匹配评审源码。原有效 ExecStart 参数及50-waveshare drop-in保留，通过99-onboarding增加可选认证参数，没有部署main中的其他语音实验。

发布前后的二进制均绑定已验证摘要：voice `fec31c461de9201eb9dcaef6f0c64e01a5ffce3ea62e08e1562cc86e4d35722e`，management `7b168605299d5373f34b27c3050df52b551f2c79082cdb6bd90db13b8c71c7dc`。管理/语音 PID现为2556929/2556943；旧启动4139310、Caddy2415423保持，仅reload Caddy。语音 ready/memory_ready、active0、max2、identity1、诊断均false。5份环境/产品/设备身份/档案摘要保持，原绑定与正文起点保持；312份源 session/记忆文件集合及摘要/大小/mtime全部保持。

公网42项路径/方法/来源检查通过，TLS1.2/1.3正常校验、HTTP308跳转，证书SAN为robot.songguoxue.com、到期2027-02-07 03:41:43 UTC。Mac外部客户端再次确认healthz200、session401、内部认证404且证书验证结果0。未登录/未认证接口拒绝，内部/原始记录路径404；不以连通200当设备绑定成功。正式owner登录仍待用户客户端检验，未索取或重置其密码。

私有备份在 `/var/backups/family-robot/onboarding-20261109-r1`，含管理配置/链接/单元、实际voice ExecStart与drop-in、Caddy和保护元数据；312份正文/记忆仅保存摘要/大小/mtime，未复制正文。发布脚本另经独立复审与云端隔离故障注入，修正Caddy回退权限、best-effort恢复及恢复验证后通过；没有以生产回退演练冒充已验证。云端deployment.json、final-preservation.json、public-verification.json保留实际证据。

云端自动检查目录为 `/home/ubuntu/onboarding-20261109/`；最终固件证据在 `task-3/evidence/{m5-final,waveshare-final}/manifest.json`，源码2aab4f4。M5 ZIP摘要83361b742f87067f1b59cbd42ad0b894513a558a0cc7bf228c8a560d57285ed4，微雪225fc24ba18d869fe59b7c25e732a6a6b9a34e7af21687a1609d799f255bead1。清理本次已复验且已展开的下载缓存约727MB，保留工具链、SDK、源码和证据；云盘约4GiB可用。只保存状态、公开版本与摘要到仓库；凭据、原始音频、二维码、NVS、固件产物及私有配置正文不提交。

## 手机与设备待验收

已由账号持有者完成开发者工具登录，并导入正确工程/生成预览；仍需核验真实家庭登录、测试号BLE能力及实际手机权限。合法域名校验保持开启。正式小程序码生成须有云端有效凭据和对应上传版本；截图已暴露的旧AppSecret不复制或使用。备案当前有效与正式发布资格未在本批核验。

Android/iOS 真机扫码唤起、App/page 生命周期、BLE 权限、20字节分包、真实 Security2、Wi-Fi 错密/重试/断连、180秒到期、TLS/校时恢复、设备独立确认、断电恢复和后续播放均未实测。现有微雪家庭绑定起点仍是2026-11-09 10:05:08（北京时间）；尚未用当前设备验证绑定后新正文/播放状态，继续保留待验收。

交付的小程序包只可作为开发者工具导入客户端，不是普通网页地址；无工厂身份和配套固件的当前设备无法直接完成此流程。见[部署与供应](../../deploy/onboarding/README.md)、[客户端](../../web/miniprogram/README.md)、[固件](../../firmware/ONBOARDING.md)。

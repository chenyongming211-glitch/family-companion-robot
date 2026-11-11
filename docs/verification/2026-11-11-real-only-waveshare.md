# 管理真实入口清理与微雪唯一目标

状态：代码、专项验证、最终审查、云端清理/退役/静态发布及真实浏览器复核完成。原生固件CI首次因构建容器DNS失败，尚未进入编译；同SHA第二次仍为相同DNS/apt失败，原生构建未验证。没有新刷机或实机验收。

用户于2026-11-11要求删除此前完整管理mock、样例数据和演示入口，不留另一份演示产品；M5已退货，当前只保留微雪ESP32-S3 2.16。范围见[设计](../superpowers/specs/2026-11-11-real-only-waveshare-design.md)和[计划](../superpowers/plans/2026-11-11-real-only-waveshare.md)。隔离测试输入、必要S3共用实现及历史固件证据保留；不改写Git历史，不把真实聊天、记忆、凭据或设备备份作为mock删除。

## 实现与代码验证

- 前端4995d80/8a3d13a删除mock adapter、seed、演示专用组件/操作页/样式、假默认ID与模式开关；api始终调用同源真实Remote API，错误类独立。未接入写操作的模拟表单移除，布局保持。原型8文件在172892a删除。
- 复核曾发现登录子组件在隐私清理卸载后丢失账号变更事件，8a3d13a用App监听的稳定事件修复。真实App/DataPages/Remote API集成4项先RED后GREEN，包含同home hash登录及退出，无伪造hashchange或前后台切换补救。
- 云端最终52项测试零失败/跳过，类型/默认构建通过；旧环境变量设置为mock仍生成相同真实产物。33项输入Mac与云端逐项相同。source manifest文件SHA `691fa7c3ab6939e5aacb15b865f2aabfff45db986595950f71d6493fceb44e28`，dist manifest文件SHA `35e6c381371b99e2ea5096e69210d214c896a488428c74d15c2bc0e219bba7c4`。这两个是JSON清单文件摘要，不是另一种JSON规范化算法的摘要。
- 真实产物4文件：index.html、family.html、assets/main-kQkgunfs.css、assets/main-8hZxc4FT.js；JS SHA `e378df70541097e4534951c200eea19833570a48b005938ddc18eaf0116a186d`。前端任务规格/质量审查PASS。
- 固件2079ac0改活动矩阵为唯一Wave产品目标，原版基线也实际选择锁定上游Wave原版build名。上游归档/IDF/依赖pins不变，信息性board/variant更新；验证工具保留真实编译来源、依赖、包名和Flash约束。新工厂供应、CLI及NVS镜像只接受微雪，历史M5配置仍可读。没有改通用S3运行逻辑、语音、蓝牙协议或板级驱动。
- 云端完整host151项：149通过、2原有未配置native candidate可选跳过。此前ASAN环境、checksum匹配公开fixture及上传遗漏导致的失败日志保留，未放宽产品校验。461份公开输入前后不变并与Mac一致，manifest SHA `924a8fe7d8600cc5c710e7ca831b22701b3467a095653f44a5020b62d24fe8a3`。
- 固件复核P2指出旧M5 evidence suite退出后丢失共用唤醒资源负例；aa26b17仅在Wave测试补5个错误命令/模型/Kconfig/损坏容器用例，对应文件19项通过，活动套件收集156项。151完整结果属于补测前快照，不冒充最终156完整PASS；实际CI另列。
- 云端management相关race15顶层/18含子例，CLI全包10顶层/30含子例、vet/build通过；大管理全包主动取消，未计为PASS，相关进程已回收。只编译新CLI，无管理二进制部署需求：HTTP不调用供应入口。

## 实际运行清理与保护

云端根目录 `/home/ubuntu/real-only-waveshare-20261111`。仅静态发布与既有ConfigStore单一M5 Binding.Until事务；不重启Voice/管理，不刷机/开USB，不改Caddy/防火墙，不回写旧配置。

- 只读盘点确认10处旧前端缓存副本378文件、4处完整原型副本32文件与旧release静态65文件，精确manifest合计475。当前新真实构建不在删除清单，共用物理node_modules明确排除。先发布新真实4文件，活动HTML两项替换并按新pins保留；浏览器通过后，实际删除其余473个旧公共文件（378缓存前端＋32原型＋63旧静态），无归档mock副本。
- 扩大盘点15个已知日期工作目录，发现的前端/原型均纳入精确清单。mini-final.tar.gz（84成员）与uiux-reference.tar.gz（30成员）仅在RAM检查成员名，没有管理mock/原型匹配，保留且未提取。未知私有路径不扫描、不删除。
- 旧18790 Python PID1780489经exe/start/cwd/argv/socket归属复核后通过pidfd TERM停止；随后精确取消本机18790转发。SSH PID85103继续监听18791，两个真实HTML的HTTP SHA/no-store在本机转发核对通过。云端IPv4/6均无18790监听。
- M5一次性事务工具6项云端race、vet/build通过，涵盖当前M5拒绝/微雪保持/历史M5仍可读/工厂鉴权失效、重复与歧义拒绝及私有计划文件。清理脚本6项云端保护测试通过，含未知路径/依赖/符号链接/摘要漂移拒绝，以及发布失败仅从RAM恢复HTML、不归档旧mock。执行前独立运维规格/质量审查PASS。
- 初始298候选只读预检摘要全部匹配，扩大后的475候选执行前再次全量预检通过；保护快照包括9个非目标保护文件、4573个记录/记忆文件、4个服务进程/argv/env、权限/RW/socket/锁、Caddy字节和元数据及原审计前缀。实际publish-retire和purge各自操作窗口前后保护全部通过：进程/二进制/argv/env、记录/.memory、非目标配置及偏好、权限/RW/socket/锁、Caddy字节/元数据、防火墙本批前后摘要均保持；原审计359865字节前缀仍通过，只允许真实新访问追加。

- M5单一绑定在`2026-11-11T14:52:19.143176542Z`结束，事务无写入错误；只有对应Until变化，历史绑定及真实档案保留，当前所有账号/作用域M5授权拒绝。微雪原绑定与当前20/20偏好未改，工厂准入随现有activated检查失效；没有删除工厂凭据/真实记录或重置家庭。
- 当前管理仍PID1401849 / `b49d95542359d5cf07d549145d5acee567e168bdc426775700d9338908a0ba9d`，Voice仍PID1161080 / `eab47a77110548a09d4dc0340c6a52660d9a7a926c587d220640ff95672e3519`，ready/memory_ready为true，收尾active0；旧Go/Caddy进程保持。没有服务重启、Caddy reload、防火墙写入、实际音量/亮度操作或记忆清除。
- 当前static为原release/web，实际文件集合正好等于新4项pins，旧3份JS/CSS及其他旧release UI已删除。新公开来源清单在release父目录`real-only-frontend.json`，此前frontend-login-fix.json只作历史证据。旧release二进制/证明保留，但其旧UI目录已清空，不能把它们作为可用界面回退；应重建当前真实前端，禁止恢复已删除mock或整份回写旧配置。
- 两个物理node_modules及Node工具链保留。云端4个原型目录和本机原型目录均已移除；完整审查diff中含已删除mock源码，审查后6份scratch diff也已删除，仅保留报告、提交引用及摘要清单。没有新建mock备份。清理仅覆盖本项目已确认公共路径，未知私有目录未做取证式扫描。

## 真实浏览器验收

Ego TaskSpace9临时验证，完成后keep[]关闭；没有操作用户保留的TaskSpace7，也没有保存密码、Cookie或聊天正文。

- 两端实际退出后，在原`#/overview`与`#/home`登录并保持同hash，App恢复真实页面；无需手工hashchange、刷新或前后台切换救回登录。沿用原账户，无重置。
- admin/family当前设备均1、型号微雪、在线；真实runtime读取和组件均显示音量20/亮度20、2滑块/22圆点，未提交控制。
- 两端真实6条聊天列表、首项5轮用户/助手面板、24条近期记忆列表、记忆详情和对应来源聊天均读取并渲染成功。仅输出数量/状态，不导出正文或截敏感截图；实际数据会随新对话变化，不把这些数量当固定产品seed。
- 内部真实审计页50条可见、无错误。第一次浏览器脚本只按`#/audit`结尾找导航，遗漏现有device查询参数；检查实际href后改选择器完成剩余审核，没有改产品代码或重复已过读取。
- 新入口无演示模式，操作保持真实授权。浏览器只读与合成检查不代表新手机、声学、屏幕效果或长期稳定性通过。

## main与实际CI

代码`aa26b17c48e9a815c41ed3475ab3925d680d25c2`已推main；最终文档没有改变被测试的执行源码。原HTTPS OAuth缺workflow scope被GitHub拒绝；使用本机已有GitHub SSH443认证推送成功，origin和全局Git配置未改，无新凭据。最终文档提交另记，不把文档HEAD说成已运行固件构建。

- [Wave原版运行43042511270](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/43042511270)与[Wave产品运行43042511138](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/43042511138)均为上述实际SHA。第一次两者在apt依赖安装失败：`archive.ubuntu.com`、`security.ubuntu.com`解析失败，随后缺少libssl-dev/cmake，exit100；prepare/host/native build/evidence均未执行，不计作固件失败定位或固件构建PASS。
- 连接器重跑403，GitHub CLI用已有授权成功请求同SHA第二次，仅重跑失败作业；没有改工作流跳过依赖或放宽校验。第二次stock作业130600363398与product作业130600361169仍均因同一DNS错误exit100，两个运行均failure；没有进入prepare/host/native build/evidence。未继续盲目重跑或改生产DNS。应在后续恢复CI网络后重新验证这两个实际目标，不能将本轮host测试或当前旧设备固件当作新native构建PASS。

## 旧发布事务边界

本批不修复或重新设定原审计发布事务基线。原事务中断、缺少deployment.json，以及防火墙原摘要 `951e883a...` 与当前 `c34cd347...` 差异仍独立未关闭，原因未证实。本批仅比较自身执行前后摘要，不更改规则，也不宣称原strict transaction验收PASS。

## 后续

先补网页设备昵称/位置保存，复用小程序既有真实接口及版本冲突/权限核验，再安排成员权限、留存与删除。当前不增加这些功能或界面重设计。合成测试与固件构建不代表新实机、听感/视觉、手机配网或生产长稳验收。

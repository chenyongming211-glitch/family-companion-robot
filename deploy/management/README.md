# 独立管理服务

**2026-11-14手动OTA管理已发布：** 管理代码新增`--firmware-root /var/lib/family-robot-management/firmware`，私有目录0700、文件0600，沿用现有管理服务可写父目录，不增加Voice写权限。使用`management publish-firmware --root <私有目录> --manifest <已核对元数据> --app <app.bin> --partition <二进制分区表>`封存可信构建；校验实际镜像版本/ELF/内容摘要、芯片/项目、精确双槽布局，禁止任意URL输入或覆盖同一发布。最多8份/共32MiB、1024设备文件、每设备32项任务，不静默删除旧证据。两端`GET/POST /api/<scope>/device-firmware/<dev_32hex>`复用当前设备管理权限；新增审计分类`firmware`。仅必要家庭/设备接口进入Caddy白名单，内部管理路径保持既有访问边界。代码、两端真实页面、云端整包和整体审查通过；管理已发布device-ota-20261114-r1，Voice保持原版，微雪首次OTA待现场确认。发布须从实际Caddy叠加三项匹配器以保留原路由，完整状态见[本轮验证](../../docs/verification/2026-11-14-device-ota.md)。

**2026-11-11真实入口清理：** 管理产品只使用默认真实构建；旧mock、HTML原型、云端旧前端/静态副本及18790演示入口已删除。当前仍由原管理进程在18791服务`admin-audit-20261111-r1/web`的4个真实文件，公开pins为release父目录`real-only-frontend.json`。M5当前绑定已结束，仅微雪为当前设备；真实旧档案和历史配置兼容保留。旧release二进制/证明不删，但其旧UI已清理，不能直接作为界面回退。详情与CI/现场边界见[本批验证](../../docs/verification/2026-11-11-real-only-waveshare.md)。

**2026-11-11操作审计：** 新增仅内部`GET /api/admin/audit`，有效admin会话、当前InternalAdmin及事件时刻明确家庭/设备授权共同决定可见行。参数仅单值`device/from/to/action/offset/limit`；带时区RFC3339半开时间区间，action为access/device_control/memory_clear/settings，offset0..10000、limit1..100默认50。返回items/hasMore/pending，不暴露原始日志或全局总数，无family及公网代理。管理已切换`admin-audit-20261111-r1`，Voice保持原版；功能及数据核验通过，原发布事务中断且后续防火墙摘要不匹配，严格全保护验收尚未完成。不要覆盖既有事务重跑fresh deploy或回写旧数据，见[验证与恢复边界](../../docs/verification/2026-11-11-admin-audit.md)。

**2026-11-11最新：** admin/family均支持`GET/PUT /api/<scope>/device-runtime/<dev_32hex>`及`POST /api/<scope>/memory-clear/<dev_32hex>`，复用既有命令/回执和唯一Voice Store。内部入口另需当前InternalAdmin；两端同样校验明确设备及CanAddDevices，清除还需正文授权。admin新接口未增加公网代理，仅现有回环/SSH访问。两服务既有`--memory-control-socket`及Voice `--management-config`保持，私有claim可选scope省略兼容family，Voice新授权含scope。实际已发布admin-control-20261111-r1；本次current链接未移动，以实际systemd exe/argv为准，管理static显式指向新release/web，旧static保留。详情见[验证](../../docs/verification/2026-11-11-admin-device-management.md)。以下日期段落和基础模板保留各自历史范围。

2026-11-10 家庭小程序新增设备/聊天只读页与真实云端展示设置。`GET/PUT /api/family/device-settings/<dev_32hex>` 读写家庭展示昵称和摆放位置，PUT 必须提交 `name,location,version`，保存后客户端重新 GET 对照值和版本。读取需当前设备授权，写入另需当前绑定家庭 canAddDevices 管理权限；事务内重新核对会话、权限与版本，冲突409。昵称1至12字、位置最多24字，拒绝控制符及大小写别名/重复/额外键。正文授权边界与语音配置不变。

设置通过管理私有配置中的可选 `devices[].display_name` 覆盖家庭列表展示，原 `name` 与 voice profile 保持；位置使用既有 `location`。不写 presentation 只读挂载，不下发音量/亮度，不宣称设备回读。网页两端仍保留原只读交互。新增配置字段与旧二进制的严格解析不兼容：用户保存后，不能直接换回不认识 display_name 的旧二进制；应优先保留新管理二进制仅回退小程序/代理，必要时先在云端私有备份最新配置，再事务移除展示覆盖字段，保留最新账户、绑定和认领，禁止整份回写旧配置。

2026-11-09 新增小程序设备认领与独立激活写接口，已发布 onboarding-20261109-r1；既有设备/聊天/设置页面仍只读。公网仅通过 Caddy 精确代理必要路径，详见[接入与供应](../onboarding/README.md)及[验证](../../docs/verification/2026-11-09-device-onboarding.md)。

本模板用于云端发布；添加模板本身不会安装、启动或重启任何服务。正式语音进程与设备固件不在本管理单元范围内。当前产品仅支持微雪 ESP32-S3 2.16，历史M5配置兼容用于保留真实档案。

所有构建、命令与配置初始化在云端执行。前端默认 `npm run build` 生成真实两端 `dist/`，无mock模式或第二套演示构建。发布目录采用 `/opt/family-robot-management/releases/<build>/`，包含 `management` 二进制与核验后的静态资源；服务使用的二进制和静态目录以实际systemd argv为准，不只看`current`链接。当前静态目录为`admin-audit-20261111-r1/web`。模板中的 `@RECORDS_ROOT@`、`@PROFILES_DIR@`、`@BUILD_ID@` 必须替换为部署时核验的路径/版本（无空格）；若无 profiles 参数，移除对应参数和 BindReadOnlyPaths 中的资料映射。只读源记录与资料路径必须位于 ProtectHome 可访问的系统路径，不移动或修改正式源数据以适应模板。

服务需要 root 读取既有 root/0700 记录目录；不得放宽源目录权限。模板将源记录目录和仅包含展示资料的 presentation 目录只读挂载到服务专属的 `/run/family-robot-management/` 视图，绕开源父目录其他属主的0700遍历限制；原数据不移动、不复制，能力集继续为空。资料源必须是经核验的窄范围 presentation 目录，不能绑定包含其他私密配置的上级目录；目录挂载确保原子替换后能读到新版本。systemd 只允许写管理服务自己的 `/var/lib/family-robot-management`，配置0600、父目录0700、审计0600，配置文件本身不能再只读 bind，否则认领的原子替换无法生效；原记录和资料继续只读。进程仅监听 `127.0.0.1:18791`，网页仍通过 SSH 转发访问；公网小程序走固定 HTTPS 代理及 `--public-origin`。公网 Cookie 为 Secure/HttpOnly/SameSite=Strict，本机 HTTP 转发兼容，不开启跨域或公网 HTTP。

显式初始化示例（占位符必须人工替换为核实设备 ID）：

```sh
install -d -m 0700 /var/lib/family-robot-management
/opt/family-robot-management/current/management init \
  --config /var/lib/family-robot-management/config.json \
  --username owner --family 我的家 --device-id '<verified-device-id>' \
  --name 小满 --model '微雪 ESP32-S3' --location 未设置
```

`init` 仅首次创建，拒绝覆盖现有配置，绑定起点固定为此次实际UTC时间，没有导入旧记录或指定历史时间的选项。它向当前终端输出一次 JSON（账号、随机密码、绑定起点）；密码不在命令参数中、不写入配置、日志、Git或Mac文件。不要将初始化标准输出重定向到普通日志或交给常驻服务。丢失输出时应由管理员单独处置，不用重复 init 覆盖权威配置。配置只存600000轮PBKDF2-SHA256摘要和随机盐。

会话保留在内存中，重启须重新登录；两端分别退出。每IP每分钟最多10次登录尝试，同时最多2次密码校验；最大1024会话、4096限流IP条目，达到上限明确拒绝。正文读取先完成权限校验和私有审计追加/fsync，失败则503且不输出正文。初版审计为UTC、账号ID、入口、操作、设备ID、记录ID六字段；操作审计版新增schemaVersion及可选绑定、目标、操作ID、受控结果元数据，无正文或凭据，原行不改写。新请求先持久化，结果追加失败不能触发重新执行。读取日志限root普通0600、32MiB/100000行/每行4096字节；不完整尾行pending，坏整行或超限503。磁盘空间与审计留存仍由运维维护，服务不自动删除。

2026-11-09基础版本中，专用 onboarding/activation 接口可创建认领与新绑定，其余业务API只读；后续已发布控制/清除以上方最新记录为准，其他设备/家庭/账号编辑与删除仍未接入。近期记忆使用原记录根目录内.memory快照和相同正文/绑定授权，不增加挂载，不调用recordstore.Open/Load，不写源快照或锁。内部管理员仍须明确家庭/设备授权才能读取正文。静态服务只提供 index.html、family.html 和 assets，禁止目录列表、隐藏文件和符号链接；未知 SPA 路由由前端处理。

验证门槛：云端 `go test -race ./internal/management ./cmd/management`、`go vet ./internal/management ./cmd/management`、`go build -o <private-output>/management ./cmd/management`，配合已核验的 live 前端。部署后的浏览器验收与用户新实机发言验证分开记录，合成测试不代表现场验收。

业务聊天使用`GET /api/{scope}/chats`与`/chats/<chat_64hex>`，依据有效user_text和结算事实归档，列表也须正文许可；设备过滤和分页沿用连接接口，详情和列表均先写无正文审计。原`/conversations`保留来源兼容，记录与.memory目录挂载和账号/绑定配置不变。归档没有另存正文或删除源，缺失终态/部分尾行保留不确定；不修改语音模型上下文。

# 独立只读管理服务

本模板仅供后续发布使用；添加模板不会安装、启动或重启任何服务。正式语音进程与设备固件不在本服务范围内。仅支持已确认的 ESP32-S3 设备。

所有构建、命令与配置初始化在云端执行。发布目录采用 `/opt/family-robot-management/releases/<build>/`，包含 `management` 二进制及前端 **live** 构建 `dist-live/`；`current` 指向核验过的发布目录。不要把 mock 构建部署到真实入口。模板中的 `@RECORDS_ROOT@`、`@PROFILES_DIR@`、`@BUILD_ID@` 必须替换为部署时核验的路径/版本（无空格）；若无 profiles 参数，移除对应参数和 BindReadOnlyPaths 中的资料映射。只读源记录与资料路径必须位于 ProtectHome 可访问的系统路径，不移动或修改正式源数据以适应模板。

服务需要 root 读取既有 root/0700 记录目录；不得放宽源目录权限。模板将源记录目录和仅包含展示资料的 presentation 目录只读挂载到服务专属的 `/run/family-robot-management/` 视图，绕开源父目录其他属主的0700遍历限制；原数据不移动、不复制，能力集继续为空。资料源必须是经核验的窄范围 presentation 目录，不能绑定包含其他私密配置的上级目录；服务通过 `/run/family-robot-management/presentation/profiles.json` 读取资料，目录挂载确保源文件原子替换后能读到新版本。systemd 限制写入管理服务自己的 `/var/lib/family-robot-management`，配置0600、父目录0700，审计0600；原记录、资料和配置设置为只读。仅监听 `127.0.0.1:18791`，客户端通过 SSH 转发访问，不直接暴露互联网，不开启跨域。Cookie 因此使用 HTTP、HttpOnly、SameSite=Strict；不要将此模板直接改成公网 HTTP。

显式初始化示例（占位符必须人工替换为核实设备 ID）：

```sh
install -d -m 0700 /var/lib/family-robot-management
/opt/family-robot-management/current/management init \
  --config /var/lib/family-robot-management/config.json \
  --username owner --family 我的家 --device-id '<verified-device-id>' \
  --name 小满 --model '微雪 ESP32-S3' --location 未设置
```

`init` 仅首次创建，拒绝覆盖现有配置，绑定起点固定为此次实际UTC时间，没有导入旧记录或指定历史时间的选项。它向当前终端输出一次 JSON（账号、随机密码、绑定起点）；密码不在命令参数中、不写入配置、日志、Git或Mac文件。不要将初始化标准输出重定向到普通日志或交给常驻服务。丢失输出时应由管理员单独处置，不用重复 init 覆盖权威配置。配置只存600000轮PBKDF2-SHA256摘要和随机盐。

会话保留在内存中，重启须重新登录；两端分别退出。每IP每分钟最多10次登录尝试，同时最多2次密码校验；最大1024会话、4096限流IP条目，达到上限明确拒绝。正文读取先完成权限校验和私有审计追加/fsync，失败则503且不输出正文。审计只有UTC、账号ID、入口、操作、设备ID、记录ID，无正文或凭据；请由后续运维维护磁盘空间与审计留存，服务不自动删除。

API只有登录/退出会修改会话，其他公开能力均只读。设备、家庭、账号和聊天的修改、删除、记忆及硬件操作不支持。内部管理员仍须明确家庭/设备授权才能读取正文。静态服务只提供 index.html 和 assets，禁止目录列表、隐藏文件和符号链接；未知 SPA 路由由前端处理。

验证门槛：云端 `go test -race ./internal/management ./cmd/management`、`go vet ./internal/management ./cmd/management`、`go build -o <private-output>/management ./cmd/management`，配合已核验的 live 前端。部署后的浏览器验收与用户新实机发言验证分开记录，合成测试不代表现场验收。

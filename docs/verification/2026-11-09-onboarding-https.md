# 小程序HTTPS连通入口验证

日期：2026-11-09，Asia/Shanghai。发布标识：`https-20261109-r1`。本次交付仅为域名与HTTPS入口，不是扫码注册或蓝牙配网完成。

## 用户提供与实际范围

用户提供songguoxue.com并说明曾申请备案，随后截图确认robot、api两个A记录均指向82.156.48.111，小程序request合法域名为https://robot.songguoxue.com。备案通过及当前有效状态未由本次检查确认，截图账号仍为测试号。

本轮仅为robot子域名复用服务器已安装的Caddy v2.11.9与原`caddy.service`。未安装依赖，未接入注册、账号写入或设备配网，没有将私有管理、Mac登记/配对或语音端口代理到公网。api子域名没有配置站点或签发证书。

配置来源：[deploy/onboarding/Caddyfile](../../deploy/onboarding/Caddyfile)。根路径GET/HEAD显示“HTTPS连接已就绪，扫码添加设备功能开发中”；healthz返回以下连通状态，其他请求404：

```json
{"https":"ok","onboarding":"not_implemented"}
```

## 部署前核对

- 权威DNS dns19/dns20.hichina.com及公共递归1.1.1.1均返回robot的正确A记录；云端解析同时确认robot、api均为该服务器。没有查询到robot的AAAA地址。
- 本机递归解析此前缓存NXDOMAIN，权威及其他递归已生效；不能根据本机单一缓存结果要求用户重填记录。外部连通首次使用curl的`--resolve`固定已核验IP，仍正常校验域名与完整证书链，没有使用`-k`。
- 云端80/443原来无监听，Caddy与Nginx均inactive/disabled；`/etc/caddy/Caddyfile`不存在，Nginx无已启用站点。本轮未覆盖其他运行站点，也未启停Nginx或certbot.timer。
- 保护基线含6份文件摘要：旧启动环境、Realtime环境、产品身份、设备身份库、展示资料、管理配置。原语音、管理及旧启动PID/启用状态一并记录。
- Caddy原配置目录和基线仅在云端私有`/var/backups/family-robot/https-20261109-r1/`留存。没有复制服务器密钥或私有配置到Mac/Git。

## 检查与结果

- 以caddy用户执行`caddy validate --config <candidate> --adapter caddyfile`通过；候选与正式配置为同一份文件。仅启用并启动原有caddy.service，原3个产品服务未重启。
- Let's Encrypt证书签发成功，SAN为robot.songguoxue.com，签发者YE2；有效期UTC2026-11-09 03:41:44至2027-02-07 03:41:43。Caddy负责后续自动续期，本轮不能预先宣称未来续期已经执行成功。
- 外部Mac客户端经有效证书校验，HTTPS healthz返回200及上述JSON，HTTP访问healthz返回308且Location为同域HTTPS路径。
- 收尾时本机正常递归解析已刷新；2026-11-09 12:43:05北京时间，直接以完整域名访问根路径与healthz均为HTTP/2 200，没有固定IP解析覆盖或跳过证书校验。
- 云端30项路由/方法检查通过：根路径与healthz的GET/HEAD为200且HEAD无正文；两路径的POST/PUT/DELETE为404；admin/family登录、onboarding、activation、devices、pair、xiaozhi/v1、.env、index.html、family.html的GET/POST均404。
- 云端使用操作系统信任链和域名校验分别强制TLS1.2、TLS1.3连接成功。成功页面具备no-store、nosniff、no-referrer，HTTPS响应不带Server头。
- 原6份文件摘要相同；原语音PID1319282、管理PID2343492、旧启动PID4139310及服务状态保持。语音ready/memory_ready为true、active_sessions为0，仍为58c14cd-window600-chick-dialogue-3ec5672-r1。
- 完整状态/证书公开元数据及保护摘要结果保存在上述云端私有目录的verification.json。没有记录Wi-Fi密码、AppSecret、长期设备凭据或对话正文。
- Caddy格式校验一致，云端正式配置与仓库源文件SHA256均为78cc1025ded0d0012a2fb737d088a76346d7b8e9a2333f0adb8bdef49c4c664c。临时候选文件已移除，服务为active/enabled。

## 初轮未通过与修正

- 首次用caddy用户读取ubuntu私有home中的候选被权限拒绝；转至可读的/etc/caddy候选路径后重新校验通过，没有放宽home权限。
- 证书尚在签发时初轮TLS探测返回internal alert；待ACME完成后重新以正常证书校验确认200，没有以跳过证书校验替代验收。
- 首版临时检查脚本没有将HEAD方法传给HTTPResponse解析器，错误地按Content-Length读取正文而报IncompleteRead。修正检查脚本后完整30项通过，产品配置未为此改变。
- 网页检索工具无法打开新域名，不作为连通成功证据；实际证据来自外部curl与云端TLS/HTTP检查。本机最初正常DNS访问因负缓存失败，收尾正常解析访问已单独核验200，不用固定IP探测冒充递归缓存已刷新。

## 后续边界

DNS/微信白名单保存、HTTPS200、有效证书分别只证明相应接入层工作。真实小程序登录/权限、一次性认领、设备认证与原子家庭绑定、官方蓝牙协议互通、两种S3构建及Android/iOS真机配网仍需按[设计](../superpowers/specs/2026-11-09-device-onboarding-design.md)继续开发和验证。现有设备仍使用原18041语音入口，没有迁移WSS、修改记忆或操作硬件。

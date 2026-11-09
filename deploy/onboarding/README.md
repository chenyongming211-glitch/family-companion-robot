# 小程序HTTPS入口准备

**2026-11-09部署状态：** `https-20261109-r1`已部署到现有Caddy，域名证书签发及外部连通、云端30项路由/方法与TLS1.2/1.3核验通过。只完成接入层，注册业务尚未提供。见[验证记录](../../docs/verification/2026-11-09-onboarding-https.md)。

`Caddyfile`目前只提供`robot.songguoxue.com`的HTTPS连通检查；小程序注册、蓝牙配网和家庭绑定接口尚未接入。公网根路径显示开发中，`GET /healthz`返回：

```json
{"https":"ok","onboarding":"not_implemented"}
```

只有这两个路径允许GET/HEAD，其他路径与写操作返回404。没有反向代理到管理18791、设备18041、旧启动8000或Mac登记/配对接口；不能将这里的200视为设备已注册、已联网或已绑定。小程序`request`合法域名为`https://robot.songguoxue.com`，当前不需要`socket`、上传、下载或预连接域名。

## 云端部署

代码/Git在Mac处理，运行时校验与部署在云端。复用已安装Caddy及系统提供的`caddy.service`，不新增Web服务进程或依赖；证书及ACME账户资料只留云端Caddy的私有存储，绝不下载或提交Git。

1. 核验权威DNS及云端解析：`robot.songguoxue.com`的A记录应为目标服务器，检查没有指向其他服务器的AAAA记录。用户额外添加的`api.songguoxue.com`不自动获得本站路由或证书。
2. 确认80/443无其他监听、既有Caddy配置及站点不会被覆盖，记录语音/管理/旧启动服务的PID、健康与保护文件摘要。只在云端私有目录备份原配置和服务启用状态。
3. 将候选文件传至云端，使用`caddy fmt --diff`及`caddy validate --config <candidate> --adapter caddyfile`校验。Caddy使用已配置的域名自动申请Let's Encrypt证书、管理续期，80端口提供ACME验证及HTTPS跳转。
4. 安装候选为`/etc/caddy/Caddyfile`，启用并启动原有`caddy.service`。如存在旧配置或运行中的其他站点，先合并并核验各站点，不能直接覆盖。
5. 从外部使用正常证书校验访问根路径、healthz，检查HTTP跳转、TLS1.2/1.3、证书域名/有效期，以及未知路径、写操作和私有管理/登记路径均404。外部80/443不通时检查云安全组；不能靠关闭证书校验作为验收。
6. 再核对正式语音/管理/旧启动PID、健康和保护文件摘要。验证记录只保存状态、版本、摘要及证书公开元数据，不保存凭据或私有配置正文。

当前未向微信提交代码或修改账号/服务器域名配置；用户自行完成的DNS和微信白名单只证明配置项已保存。备案通过与当前有效状态未由本次连通检查确认。

## 回退

停用本次启用的Caddy，恢复云端备份的配置与原服务启用状态。不要操作原语音、管理或旧启动服务，不删除证书存储或域名解析。若后续新增站点，应按当时完整配置回退，不能直接套用本次初始空配置状态。

后续真实注册发布前，依照[扫码添加设备设计](../../docs/superpowers/specs/2026-11-09-device-onboarding-design.md)完成权限、独立设备认证、一次性认领、原子绑定与公网接入校验，再按明确路由逐项接入；不直接开放整个私有管理后端。

参考：[Caddy自动HTTPS](https://caddyserver.com/docs/automatic-https)、[官方systemd服务用法](https://caddyserver.com/docs/running#using-the-service)。

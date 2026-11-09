# 云端生成设备小程序码

`onboarding_mini_code.py` 是操作者在现有云服务器运行的 Python 3 标准库工具。它读取供应 CLI 生成的私有工厂包，将 `claim_code` 作为微信平台小程序码的 `scene`，固定打开 `pages/index/index`。工具不访问管理服务、不注册设备、不写绑定，也不上传或发布小程序。

2026-11-09 已在云端使用用户安全填写的新私有配置，核验固定测试 AppID `wxb3f1745f8ed1fa1d` 的 stable_token 与真实发码接口；当前微雪暂存私有包已生成匹配的 develop 屏幕码。手机版本打开、扫码与发布资格仍待验证，接口成功不能代替这些步骤；见[迁移验证](../docs/verification/2026-11-09-waveshare-legacy-migration.md)。截图中的旧 AppSecret 不作为凭据，不要抄入源码、聊天、Mac 或命令行。

在云端由操作者用安全编辑方式准备 JSON，字段严格为 `appid` 和 `secret`；AppID 格式为 `wx` 加 16 位小写十六进制，AppSecret 为 32 位十六进制。凭据文件和原工厂包都必须由运行用户持有、权限 `0600`、单硬链接且不能是符号链接；每个输入最多 16 KiB。工具拒绝重复字段、未知字段和工厂包格式错误；不会把其中 `device_key` 用于请求、认证或输出。

输出目录必须已存在、由运行用户持有且为 `0700`，路径各级都不能是符号链接。示例仅在云端运行，路径按实际私有目录替换：

```sh
umask 077
mkdir -p /home/ubuntu/private-mini-codes
chmod 700 /home/ubuntu/private-mini-codes
python3 tools/onboarding_mini_code.py \
  --bundle /home/ubuntu/private-factory/device.json \
  --wechat-config /home/ubuntu/private-wechat/config.json \
  --environment trial \
  --output /home/ubuntu/private-mini-codes/device.png
```

`--environment` 必须显式选择 `develop`、`trial` 或 `release`；开发版/体验版使用 `check_path=false`，正式版使用 `true`。`false` 允许平台不校验已发布页面，并不保证账号权限、对应版本和实际手机扫码可用。正式版需发布后有此页面。

工具仅向 `https://api.weixin.qq.com/cgi-bin/stable_token` 和 `https://api.weixin.qq.com/wxa/getwxacodeunlimit` 发出 HTTPS POST；普通 token 模式 `force_refresh=false`，TLS 证书和主机名验证开启，单次网络操作超时 10 秒，不使用代理环境变量、不跟随重定向，没有可改 API 主机/页面的 CLI 或配置字段。token 仅保留在进程内存，无日志或缓存文件。

返回的 PNG/JPEG 须同时匹配 HTTP 类型、文件签名和结尾标记，总长不超过 1 MiB；JSON/HTTP 错误不会保存成图片。工具不内置完整图像解码器，生成后仍需实际扫码检查页面与 `scene`。扩展名由输出路径指定，平台可能返回 PNG 或 JPEG，以实际文件签名为准。

输出以 `O_EXCL` 新建、权限固定 `0600`，文件与目录均执行 `fsync`；现存文件、符号链接和并发创建的文件不会覆盖。失败返回非零并仅显示通用错误，正常返回零并仅显示通用创建提示；路径也不回显，避免操作者把私有值放入文件名后泄漏。写入失败会移除本次创建的文件；若操作系统连清理都拒绝，应由操作者检查私有输出目录，不能把残留文件当成成功结果。

Python `generate` 在写入或同步期间收到可捕获的 `KeyboardInterrupt`/`SystemExit` 时，也先移除本次输出，再原样抛出中断。CLI 的 Ctrl-C 仅显示通用取消提示并返回 `130`，不打印 traceback。`SIGKILL` 或断电无法执行 Python 清理；此时应由操作者检查私有输出目录。

小程序码包含认领凭据，应按工厂包同等保护，禁止提交 Git、放公开文件服务或随意转发。普通明文二维码不能替代微信平台小程序码。自动测试只用合成工厂包、合成 token 和假 HTTPS 连接；实际云端接口与设备码供应另行留证，真机扫码仍待验收。

专项测试只能在云端执行：

```sh
python3 -m unittest discover -s tools/tests -p test_onboarding_mini_code.py -v
```

接口语义已核对微信[稳定版 token 文档](https://developers.weixin.qq.com/doc/subscription/api/base/api_getstableaccesstoken.html)、[不限量小程序码文档](https://developers.weixin.qq.com/miniprogram/dev/server/API/qrcode-link/qr-code/api_getunlimitedqrcode.html)，以及腾讯 [CloudBase 参数化分享示例](https://docs.cloudbase.net/en/recipes/add-share-with-params-miniprogram)。本工具使用现有服务器直接 HTTPS 调用，无需启用示例里的云函数服务。

## 微雪屏幕码（云端）

默认 `image` 继续原样保存平台图片，不需要 Pillow。屏幕输出仅在云端隔离环境安装
`python -m pip install -r tools/onboarding_screen_requirements.txt`，并在相同命令追加
`--format screen`，输出例如私有目录中的 `device.frc`。无需先生成或保留中间图片。
该模式向同一官方接口显式请求 `width=280`、`auto_color=false`、黑色
`line_color`、`is_hyaline=false`。拒绝损坏、多帧或尺寸不匹配的图片；不缩放，
透明像素合成白底，灰度小于128记黑，不抖动。父目录0700，文件0600，失败/取消清理输出。

FRC1为9848字节：`FRC1`、LE16宽280、LE16高280、ASCII设备ID36字节、
LE32载荷CRC32、9800字节逐行MSB优先位图（1黑0白，每行35字节）。CRC仅检测损坏。
`firmware/tools/provision_factory_nvs.py` 的既有供应命令可追加 `--screen-code /私有目录/device.frc`；
仅微雪支持，严格检查尺寸、长度、设备ID、CRC与私有文件权限，写入
`family_factory/screen_code` blob。仍不写原始claim，真实NVS生成器使用微雪的0x6000分区。
该文件包含可认领设备的私有码，和工厂包同等保管，不得提交Git或下载到Mac。

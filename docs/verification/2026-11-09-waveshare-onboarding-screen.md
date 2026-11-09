# 2026-11-09 微雪屏幕扫码配网

用户要求补齐设备端首次添加页面。本批实现提交为 `c22b807a7154f35d18768f94f5deaa7f8d0d47d4`（含4d540a5供应、295a202屏幕/状态、c22b807离线字库修正），直接main。依赖、测试、原生构建和离屏渲染均在云端；Mac只处理代码/Git和公开源码、合成画面的查看。没有真实工厂供应、平台码生成、刷机、麦克风、模型调用或服务部署。

## 实现

- 复用官方微信码工具，新增screen输出：同次官方图片固定请求280px/黑码线/白底，经Pillow转换为FRC1，不缩放、不用普通QR替代平台码。默认原图输出保持。
- 供应与固件分别校验9848字节、280×280、device_id与CRC，写入微雪工厂NVS的screen_code。原始claim字符串及AppSecret不写入设备；码图本身是私有首次认领材料，仍须保护。
- 微雪显示持久的离线设备码、连接Wi-Fi、校时、云端确认、Wi-Fi错误、超时、配置/启动错误及成功。IP/BLE完成不算成功；独立认证确认或合法bootstrap恢复后才成功，2秒后恢复机器人。普通已激活启动不显示码。M5与普通固件无新增配网页面。
- 拒绝的位图在任何显示之前被清空，并由显示入口独立核对ready；旧epoch/超时不能覆盖成功。已保存Wi-Fi、云端确认后丢失本地receipt的恢复分支也显示真实进度和结果。
- 专用静态30px Noto Sans CJK SC小字库保证首次联网前的全部中文，避免依赖稍后加载的资源字体。82字形/68017字节A8像素，SIL OFL许可；源字体SHA256 `b76b0433203017ca80401b2ee0dd69350349871c4b19d504c34dbdd80541690a`。固定Pillow12.0.0，生成器检查TTC第2张字体为SC；公开字体源码可重现，C源码SHA256 `baeb8b2a817d34bc758796f567a860f9c4b5e36920428f04f0e57a785c535fb3`。

## 验证

| 范围 | 结果 | 边界 |
| --- | --- | --- |
| 码工具 | 原16项+新增4项，共20通过 | 合成平台响应；无真实微信调用 |
| 工厂供应 | 原6项+新增2项，共8通过；锁定官方SRP/NVS0.3.0真实生成0x6000镜像 | 合成身份；不是实际设备NVS写入 |
| 完整S3/共用host | 142项、零skip通过，165.747s；只选支持的S3/共用测试 | C3已退休，无C3构建/门槛 |
| 新屏幕专项 | 解析/状态ASan+UBSan、实际RenderSetup、实际Event/时钟、实际view几何/所有权、字形覆盖通过；均有对应缺失功能/缺陷RED | view几何测试用LVGL边界替身；恢复分支另经源码复审 |
| 实际LVGL9.5 | 原生软件渲染，78400个码区像素逐点一致；100次更换输入/更新/隐藏/销毁刷新；10种状态文字高度44/88px适配；理想480圆屏外非背景像素均0 | 合成斜线位图，不能扫码；未验证实体面板活跃区或相机识别 |
| 源码复审 | 供应、屏幕/状态及整体复审通过 | 真实设备与平台权限仍另验 |

实际渲染曾发现LVGL9.5需显式缓存头文件、内置首屏字库缺字及九字错误文案的圆屏边沿裁切；补正确头文件、独立SC字库，并改为八字文案后复验通过。不是只检查WXML/图片设计稿。字体生成器在固定环境重跑，完整C源码与记录摘要一致。

## 原生候选

微雪最终原生build/merge通过：app3467120字节，两个OTA槽各3604480字节，余137360字节（约4%，工具给出容量接近提示）；assets5376385/6291456。二进制分区表、SDK有效安全配置、来源清单及实际merged各flash区域逐字节校验通过；最终ZIP与merged内容一致。固件专用字体为静态flash数据，码显示拥有10088字节I1缓冲。

M5同源原生build/merge/ZIP和相同实际产物核验也通过，app3408416/4128768，余720352字节，assets5376385/8388608；没有新增M5配网页面。常规CI精确c22b807的[四S3工作流](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/42598464326)与[stock工作流](https://github.com/chenyongming211-glitch/family-companion-robot/actions/runs/42598464306)已全部success，分别为4个S3 job与1个stock job；它们与本批新工厂候选的云端两板构建分别记录，均不代表现场验收通过。

证据根目录 `/home/ubuntu/onboarding-screen-20261109/`：`source/`、`candidate/`、`candidate-m5/`、`render/`、`supported-host-final.log`、`build-waveshare-final.log`、`build-m5.log`、`evidence/{waveshare,m5}/manifest.json`。微雪实际候选ZIP在 `evidence/waveshare/waveshare-screen-c22b807.zip`，未下载到Mac；M5对应ZIP位于evidence/m5/m5-compatibility-c22b807.zip。独立新鲜prepare与构建源一致，无凭据输入被打进通用固件。

## 未执行与下一步

后续进展：用户已在云端私有配置中填入凭据。实际核对文件owner/0600、父目录0700、字段及匹配AppID通过。仅服务器进程读取并通过固定官方HTTPS调用，stable_token返回errcode0；不限量小程序码接口实际返回280×280图片，Pillow完整解码、转换及解析FRC1 9848字节通过。诊断使用非设备的capability_probe scene，图片与位图只在RAM中，未保存、展示或复制；不为其建立工厂/认领记录。结果分别存于证据根目录wechat-capability-verification.json与wechat-screen-capability-verification.json，内容只有状态/尺寸等无凭据数据。没有将密钥、令牌、响应正文或实际码写入日志、Git或Mac。

因此取令牌及发码接口的实际调用权限已验证；对应开发/体验版本的手机打开、参数接收、蓝牙权限与实体扫描仍待实测。开发者工具预览码与本次诊断均不能作为实际设备认领码。目前还没有为实际微雪生成可使用的码/NVS。

当前候选依旧为新工厂全量刷写，分区变化，不能对已有微雪做app-only/OTA。现用微雪已有legacy身份/家庭绑定；供应CLI刻意拒绝MAC冲突，本批没有迁移或删除旧身份、绑定、历史。对该设备实刷前须准备可回退备份、匹配码与NVS，以及明确的既有身份迁移；不能先擦除再补材料。

上述接口核验后再次只读确认：设备1、factory_devices0、claims0，原有效绑定与起点未变。已询问用户当前微雪迁移测试还是另一台未绑定S3，答复前保持真实身份与绑定不动。仅被动枚举一个USB串口，未确认其板型，也未开串口或复位。

实体屏幕、实际相机扫码、Android/iOS BLE权限与Security2、Wi-Fi错密/重试/共存、云端绑定、掉电恢复和后续语音播放全部待现场验收。候选/截图/测试通过不代表已刷或产品可交付。

完成后逐文件比较两份重复SDK缓存与既有缓存的10998个文件，均完全相同；改为复用 `/home/ubuntu/onboarding-20261109/task-3/candidate/managed_components`，回收1546089808字节。两板当前managed_components为指向该保留缓存的链接，不可清理其目标。最终二进制/ELF/配置/分区/资源/merged与ZIP逐份重核摘要保持，未删构建证据。云盘回到约2.9GiB可用；cache-comparison.json与cache-cleanup.json记录过程。

# 免费音乐来源小样本接入验证

验证跨 2026-11-12～13（北京时间）。范围是从现有中国云主机进行少量真实搜索、普通公开流获取和完整解码，判断是否值得接入；没有增加产品音源、注册服务账号、部署服务或操作微雪。

## 结论

- **Audius 暂不适合作为中文指定点播的主来源。** 本批 5 个“歌手＋歌名”请求，在所查每次前 5 个结果中，没有找到曲名和指定原唱均符合要求的候选；这不证明整个曲库不存在这些作品。
- **Audius 有真实 320 kbps MP3，可以作为独立音乐/轻音乐的补充候选。** 3 个普通公开流样本中，2 个取得完整音频并成功解码，均为 48 kHz、双声道、320 kbps MP3；另 1 个超时。没有据此确认长期可用性、录音品质或商用授权。
- **古典与儿歌需要选录音版本。** 搜索同时返回改编、短片段、付费门槛曲目和未核实上传者，不能只看作品名或上传者昵称自动选歌。
- **Commons 的云端直连尚未通过，Jamendo 尚缺 Client ID。** 前者遇到连接超时，后者明确返回凭据错误；均不能算作“没有这首歌”。
- **本轮没有新的微雪听感结果。** 当前线上音乐仍经过 24 kHz 单声道转换。320 kbps 音源可获取，不等于机器人已经以该品质播放，也不等于升级采样率必然改善听感。

## 方法与边界

基线：`main` 的 `2cb3676`；现有线上点歌来源修复为 `d3da340`。全部接口请求及 FFmpeg/ffprobe 运行在现有云主机，没有在 Mac 启动运行时服务或安装依赖。

Audius 使用[官方搜索/流接口](https://api.audius.co/v1)，没有使用账号、Cookie、API Key 或付费解锁。此次公共 GET 实际响应不代表正式应用可以一直无凭据使用；[官方接入文档](https://docs.audius.co/sdk/)仍提供 API Key 流程与免费档配额。

样本是 **20 项请求，含 15 项指定作品与 5 项场景发现**；其中“小星星”与英文 Twinkle 是同一旋律的不同语言查询，不能称为 20 首不同歌曲。5 项中文流行歌另用英文/拼音别名补查，首轮共 25 次搜索：24 次 HTTP 200、1 次超时；该超时查询单独重试后 HTTP 200，原失败保留。每次最多取 5 个候选，未遍历曲库，也没有做负载测试。

请求不经代理，搜索超时 15 秒、元数据上限 1 MiB；音频临时文件单份上限 20 MiB，读取超时 12 秒、总读取预算 60 秒，完整解码后立即删除。FFmpeg 为云端既有 `6.1.1-3ubuntu5`。只保存必要的测量摘要，不保存临时签名播放地址或音频。

## 20 项搜索观察

“候选”仅表示接口返回条目；没有通过实际播放、版本及权利来源核对的条目不记为可用曲目。

| ID | 请求 | 前 5 个候选内的实际观察 | 判定 |
| --- | --- | --- | --- |
| pop01 | 周传雄《黄昏》；Zhou Chuanxiong Huang Hun | 两种写法均 0 条 | 本批未找到匹配 |
| pop02 | 周深《如愿》；Zhou Shen Ru Yuan | 两种写法均 0 条 | 本批未找到匹配 |
| pop03 | 刘德华《17岁》；Andy Lau 17 | 中文 0；英文 4 条，均为其他曲目/上传者 | 无匹配，不能用相似词替代 |
| pop04 | 周杰伦《稻香》；Jay Chou Dao Xiang | 中文 5、英文 0；返回《髮如雪》《止戰之殤》等，未返回《稻香》 | 曲名不符；同名上传者也未认证 |
| pop05 | 邓丽君《月亮代表我的心》；Teresa Teng Moon Represents My Heart | 两种写法均 0 条 | 本批未找到匹配 |
| kid01 | 小星星 | 3 条，包括“小星仔”、其他歌曲与 46 分钟 DJ 串烧 | 无符合请求的候选 |
| kid02 | 两只老虎 | 0 条 | 本批未找到匹配 |
| kid03 | 小兔子乖乖 | 0 条 | 本批未找到匹配 |
| kid04 | Twinkle Twinkle Little Star | 首次超时；重试 5 条，含 96 秒放松改编、28 秒合成演唱候选及 141 秒 Kids Club Music 候选 | 有相关版本，演唱内容/权利来源/完整流待核对 |
| kid05 | Frere Jacques | 3 条，含音乐盒及 29 秒男声版，均有流访问门槛 | 不计为免费完整儿歌通过 |
| class01 | Mozart Eine kleine Nachtmusik | 5 条，混有电子改编、未核实署名的古典版本及受限版本 | 有相关候选，未选定录音 |
| class02 | Beethoven Fur Elise | 5 条，首项是 Dubstep remix；其他含短片段与钢琴版本 | 有相关候选；首项技术流测试见下表，不作为传统钢琴版 |
| class03 | Bach Cello Suite 1 Prelude | 5 条，前两项为钢琴账号的改编，后三项是第 6 组曲 | 未确认所需大提琴第 1 组曲录音 |
| class04 | Chopin Nocturne Op 9 No 2 | 5 条，含题名匹配及其他夜曲；演奏来源未核实 | 有相关候选，未选定录音 |
| class05 | Pachelbel Canon in D | 5 条，含管弦、替代版和吉他练习等 | 有相关候选，未选定录音 |
| scene01 | piano relaxation | 5 条，如 One Perfect Moment，上传者/具体录音许可待核对 | 场景候选，未做听感确认 |
| scene02 | ambient sleep | 5 条，环境音相关标签较多，不能只凭标签认定适合助眠 | 场景候选，未做听感确认 |
| scene03 | lullaby instrumental | 5 条，混有摇篮曲器乐和 Trap/说唱伴奏 | 需要筛选，不能整批当儿歌 |
| scene04 | rain meditation | 5 条，首项雨声相关，另有说唱、Trance、有声内容 | 首项流超时，其余不能盲选 |
| scene05 | acoustic instrumental | 5 条，首项为已认证账号的吉他器乐；另有 Beatles 翻奏及其他器乐 | 首项完整获取/解码通过，权利与听感独立待核对 |

尤其是 pop04：上传昵称“周杰倫 Jay Chou”并不证明原唱身份。现有产品的歌手完整条目匹配可以阻止“昵称含歌手名”的误选，但仅靠字符串相等仍不能证明某账号或录音为官方来源。

## 实际音频获取与完整解码

使用 `GET /v1/tracks/{id}/stream`，不访问 gated 曲目，不调用原始文件下载/解锁接口。实际两个成功流均重定向到 Audius 使用的 Cloudflare R2 音频存储；只记主机，不保存带查询参数的临时地址。

| 曲目 ID / 平台题名 | 平台时长 | 实际文件 | 实际编码 | 结果 |
| --- | --- | --- | --- | --- |
| `L5x31` / Beethoven - Fur Elise (Klutch Remmix) | 140 秒 | 5,634,285 B；140.832 秒 | MP3、48,000 Hz、双声道、320,000 bit/s | HTTP 200；读至 EOF；长度与响应一致；ffprobe 0，完整 FFmpeg 解码 0 |
| `rkZQV` / Bring It On Home [Acoustic Instrumental] | 120 秒 | 4,801,965 B；120.024 秒 | MP3、48,000 Hz、双声道、320,000 bit/s | HTTP 200；读至 EOF；长度与响应一致；ffprobe 0，完整 FFmpeg 解码 0 |
| `KKpZy` / calming rain sounds for relaxation meditation and sleep | 303 秒 | 未取得有效响应 | 未测得 | 读取超时；不能因 `is_streamable=true` 就记作能播 |

成功样本的文件时长接近平台时长，没有观察到仅取得短试听的迹象；这仍不是对曲目内容、录音完整性或原版身份的人工鉴定。

音频 SHA256：

- `L5x31`：`f3e5245c62681e4ba5c6604a552972bf726c6c549496ed3e45ef592636b34134`
- `rkZQV`：`61dead472bd7736fe81a5be622dafaa339a81d6b500dbcef8c650bce9930b2c5`

完整解码命令为 `ffmpeg -hide_banner -v error -xerror -nostdin -i SESSION_FILE -map 0:a:0 -f null -`。两份文件均在测量后删除，失败样本临时目录也已清理。会话结束核对没有残留 `audius-session-*` 目录。

`L5x31` 的平台许可字段为 CC BY；`rkZQV` 为 All rights reserved。搜索还出现 CC BY-NC-SA 的 Beatles 翻奏。Audius 的 [OML](https://openaudiofoundation.org/open-music-license.pdf)提供播放器授权与商用署名要求，也允许替代许可，并由上传者作权利承诺；**公共流可取、账号认证、许可字段都不能单独替代具体录音的来源核对**。正式接入前要落实适用许可、作者/版权/许可/原曲链接，不能把所有公开条目当成同一种免费商用资源。[API 条款](https://audius.co/documents/ApiTerms.pdf)限定必要的会话缓存，因此本轮没有长期保存 Audius 音频或建立曲库镜像。

## 其他来源

| 来源 | 实际检查 | 判定 |
| --- | --- | --- |
| Wikimedia Commons | 云端 API、文件页及上传主机请求均超时；对具体 FLAC 原文件再作 IPv4、无代理的范围请求，curl 28 / SSL connection timeout，HTTP 000、0 字节 | 云端直连未通过；未取得文件，不能声称完成了无损音频实测，也未确定超时的网络根因 |
| Jamendo | 不带 Client ID 请求 `https://api.jamendo.com/v3.0/tracks/?format=json&limit=1`，HTTP 200，但业务 `status=failed/code=5`、Invalid Client Id | 缺凭据，曲库覆盖与 FLAC 可取性未验证；没有借用他人 Client ID |

Commons 的[具体候选文件页](https://commons.wikimedia.org/wiki/File:Kimiko_Ishizaka_-_Bach-_Well-Tempered_Clavier,_Book_1_-_02_Fugue_No._1_in_C_major,_BWV_846.flac)可经资料检索工具查看：2015 年 Kimiko Ishizaka 演奏，页面列 FLAC、1 分 56 秒、约 29.79 MB。正文把**录音**标为 CC BY 3.0、**曲作**标为公有领域，结构化字段还出现 CC0，不能直接按“巴赫公版/文件为 FLAC”推定该录音可无条件使用。该文件只作候选，未导入产品。

[Jamendo 条款](https://devportal.jamendo.com/api_terms_of_use)将免费 API 使用限定为非商业用途，商业接入需另谈条件。缺少 Client ID 的结果不用于估算命中率；也没有为本轮代注册账号或付费。FMA、Pixabay、Musopen 没有增加运行时实验，原调研中的 API/独立分发/会员限制仍需按具体用途处理。

## 当前播放链路与下一步

本轮重新核对现有产品代码：

- `internal/music/source.go:143` 仍请求 `br=128000`；这是请求参数，不是每份实际文件的码率测量。
- `internal/music/stream.go:16–17` 仍把 MP3 转成 **24 kHz、单声道 PCM16，固定 −6 dB**。即使新来源提供 320 kbps、立体声或 FLAC，此路径也不会保留全部源信息。
- 现有微雪 48 kHz 试听是 USB 独占试验路径，不能作为“Wi-Fi 音乐 48 kHz 且正常语音打断”已实现的证据。此前 Carefree 的 24/48 kHz 现场反馈是“听起来差不多，没有明显异常”，不是本轮新来源的听感成绩。

建议先保留现有产品通路，**不直接全量接入 Audius**。下一小步是选出少量来源和许可可追溯的古典/轻音乐录音，再做同源、同增益的微雪听感比较；中文指定原唱的需求继续单独解决，不能用轻音乐搜索替代。48 kHz 无线播放应在音源选择与听感证据明确后另行实施，当前没有因此刷固件。

## 交付与证据

本批仅交付本报告及交接说明。原始搜索观测、失败与测量摘要在云端 `/home/ubuntu/music-source-validation-20261112` 的私有研究目录；临时音频已删除，没有复制到 Git 或建立产品缓存。检查结束目录约 113 KiB，磁盘可用约 4.94 GiB；没有占用新的固件构建空间。

没有重启/部署语音、管理服务或 Caddy；没有修改音源选择、设备偏好、家庭数据或固件。此报告记录“已验证的接口/文件结果”，不关闭已有的现场错误、整曲自然结束或音乐语义打断待办。

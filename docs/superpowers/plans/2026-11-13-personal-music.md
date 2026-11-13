# 个人点歌质量与私人曲库实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans task-by-task. 用户已批准本方案并要求开始；不重复申请执行许可。

**Goal:** 减少原唱/版本误选，提高可取得的音源码率，并提供仅授权家庭可管理、由其已绑定设备语音点播的小型私人曲库。

**Architecture:** 复用 `internal/music` 的查找、解析、FFmpeg及Session播放通路。该包新增有界私人文件库，管理服务负责家庭权限及上传/删除，语音启动入口把已鉴权设备映射到当前家庭；两者共享同一私有目录。网页设置内增加朴素曲库表单；不建独立音乐服务，不修改固件或24kHz/600ms播放协议。

**Tech Stack:** Go标准库、现有FFmpeg/ffprobe、Vue 3/TypeScript。所有运行时检查/构建在云端。

## Global Constraints

- main直接提交推送，不建分支/worktree；仅微雪为固件支持目标。
- 文件/凭据/生成音频不入Git；无产品mock、种子曲目或演示开关。
- 先验证输入，再原子保存；MP3/FLAC单份最多30MiB，库总量最多300MiB、最多100首；实际时长1秒～20分钟，单/双声道、采样率8～192kHz；禁止图片/视频/多音轨或加密容器。
- 家庭成员可读；仅当前家庭有管理权（CanAddDevices）且有设备授权的账号可上传/删除；内部管理员同样必须拥有该家庭授权，不绕过范围。
- 以当前设备绑定定位家庭，上传写前/结果输出前与语音读取时重新核验；不接受客户端传入文件路径或任意URL。
- 私人曲库始终为空开始；实际文件须由用户自行导入，不拿测试音频充当歌曲。
- 在线高码率是目标请求和实际观察，不承诺无损或听感提高；原唱仅按来源标注和严格匹配，不冒充官方身份鉴定。

## Task 1: 严格曲名/版本和在线源质量

Files: `internal/music/source.go`, `source_test.go`, `internal/realtimelive/music.go`（事实字段由后续集成统一调整）。

- [ ] RED：默认《黄昏》不接受《黄昏(Live)》《黄昏伴奏》《黄昏串烧》或更长同名；显式《黄昏(Live)》只接受匹配版本；歌手完整匹配/合作条目规则保留。歌手选集排除明确标注的现场/伴奏/翻唱/混音。
- [ ] 使用已有云端Go测试入口验证失败，然后修改匹配规则；旧宽松版本测试更新为新需求，保留错误歌手负例。
- [ ] 将解析请求 `br=128000` 改为 `br=320000`，接受平台实际降档的完整MP3；保留试听、大小、域名、SSRF、取消和20分钟边界。测试请求值、低码率合法完整源仍可用、试听仍拒绝。
- [ ] 云端定向race通过；真实《黄昏》请求320后抽样测实际文件编码/时长/完整解码，临时文件删掉；不能把请求值当实测。提交并任务审查。

## Task 2: 家庭私人曲库与语音选曲接缝

Files: `internal/music/library.go`, `library_test.go`, `library_catalog.go`、必要的 `source.go/stream.go`。

Interfaces: `NewLibrary(root, family string) (*Library,error)`；`Library.List() ([]LibraryTrack,error)`；`Library.Import(ctx,title,artist,format string,input io.Reader) (LibraryTrack,error)`；`Library.Delete(id int64) error`；`Catalog.WithLibrary(library *Library, allowed func()bool) *Catalog`。

LibraryTrack 的JSON字段固定：`id`（安全整数）、`title`、`artist`、`durationMs`、`bytes`、`format`、`bitrate`、`sampleRate`、`channels`、`addedAt`。私人数字ID与远端ID区分且低于JS安全整数上限；不泄露路径。每首独立目录、0600文件和0700目录；临时目录经ffprobe/完整FFmpeg解码核验后原子rename提交，删除仅操作校验过的该条目；进程间写锁和删除/读取并发处理，不保留部分上传。容量同时包括暂存以防并发绕过，读取不遍历其他家庭。

- [ ] RED：实际合成MP3/FLAC导入、列出、精确选曲、删除；家庭隔离、越界ID/路径/符号链接、超限/损坏/取消/并发、重启后仍可读。
- [ ] 实现库和复用匹配/解码的Catalog接缝。匹配私人条目优先；无匹配再查询在线，损坏/无权限拒绝且不泄露家庭条目。每次搜索/解析/开始播放及输出过程中检查allowed；资源只能由同一库解析/打开，不能传客户端路径。
- [ ] Stream私有文件仍经同一FFmpeg输出24kHz PCM16/-6dB；为FLAC允许仅本地pipe输入的解码参数。保留有界输出及完整性检查。
- [ ] 云端库/源race与实际FFmpeg测试通过，提交并任务审查。

## Task 3: 管理API与真实语音接入

Files: `internal/management/music.go`, `music_test.go`, `server.go`, `cmd/management/main.go`, `cmd/realtime-output-live/main.go`, `internal/realtimelive/config.go`, `music.go`及测试。

API: `GET /api/{family|admin}/music-library/{deviceId}` → `{items,canManage,maxBytes,usedBytes,maxTrackBytes}`；`POST`同路径为multipart，字段`title/artist/file`、必须用户确认文件允许个人使用；`DELETE`同路径`?id=<safe-int>`。复用cookie/Origin/session及实时Config.Authorize，解析头前先鉴权；上传并发限制、按路由延长读写deadline，失败无成功提示。管理CLI增加`--music-library`路径；语音同名flag并复用已有`--management-config`定位当前设备家庭。与记忆开关配置校验分离，避免只启用曲库时强制需要记忆socket。

- [ ] RED：未登录、只读、跨家庭/admin无grant、绑定迁移、超大multipart、伪格式、重复字段、非法ID/方法；真实文件导入→GET→语音来源查找/解码→删除后不可新播放；不会提供无鉴权文件下载。
- [ ] 仅在CLI组合层读取管理配置并构造带当前绑定校验的私人Catalog；Session仍拥有播放/切歌状态，来源不得修改设备状态。源事实新增来源标记，区分平台与私人用户填写信息；不把录音标题称为已认证原唱。
- [ ] 云端全相关Go测试/race、vet和两个二进制构建，提交并任务审查。

## Task 4: 网页私人曲库入口与发布验证

Files: `web/management/src/components/MusicLibrary.vue`、相关类型/API及CorePages settings接入、组件/API测试；实施验证与交接文档。

- [ ] RED：编译组件测试上传字段/成功后GET确认/取消删除或成功后重读/403与超限可见/切设备或退出后旧结果不回填；真实API调用测试不含产品假数据。
- [ ] 家庭“设置”和内部“配置”都挂在所选设备下，列表展示歌手、时长、格式和实际编码参数。管理者显示title/artist/file和个人使用确认，上传成功重读后展示；删除有确认。无网页试听/公开分享/复杂动画/新依赖。
- [ ] 云端npm测试、类型检查、构建；隔离真实管理服务浏览器验收家庭导入/删除与只读隔离，不在正式库植入合成曲目。
- [ ] 对照实际运行来源构建候选，保留既有未部署差异；检查磁盘/活动语音/服务身份和私有配置保护后发布管理与语音，库只创建空目录。后台读回及健康核对，用户真实文件/微雪听感作为独立待验，不伪造现场PASS。
- [ ] 最终架构与差异审查，main推送，更新交接与验证报告；只清本任务可重建缓存和临时音频，不动历史证据/备份。

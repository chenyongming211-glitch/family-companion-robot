# C3 PCM 云端隔离评估结果

## 结论与实际状态

用户在C3复验失败、稳定版恢复后选择“继续做隔离评估”。本阶段已完成：PCM可在隔离原型中接入当前共同Session，保留鉴权、Opus旧入口、样本时钟、输出ASR和取消/drain。最终全仓Go回归373项主测试通过、2项既有可选跳过；含子用例共858个pass。PCM聚焦7项主测试/19项含子用例，以及相同范围race检查均通过。

结论是**可以进入有界PCM固件设计，尚不能安装为产品候选或宣布插话修复**。真实C3内存/调度/Wi-Fi与声学效果没有在此阶段测量。C3仍运行稳定6b79e4c/app1；正式Go09d22e6及其他设备保持。本阶段没有构建、刷写或启动新固件，没有修改正式配置或清空记忆。

## 隔离方式与可重复证据

基线源码为main `2294240`。Mac只编辑代码、Git和传输源码；运行在云端私有目录 `/opt/family-robot-validation/c3-pcm-assessment-20261101/`。`source/`为实验副本，`baseline/`为未修改对照。所有HTTP/WebSocket服务均由httptest创建临时回环监听；没有新增长期服务，没有真实模型调用或设备连接。

[apply_trial.py](../../tools/c3-pcm-assessment/apply_trial.py)显式修改副本中四个文件，仓库正常产品源码未改：

- `internal/protocol/protocol.go`：新增仅供原型入口显式调用的PCM解析器，旧`ParseHello`仍只接受Opus。
- `internal/realtimelive/xiaozhi_entry.go`：保持原鉴权和归属，只记录显式协商的格式。
- `internal/realtimelive/xiaozhi_wire.go`：原生PCM帧检查、双向格式声明，复用原控制与回报校验。
- `internal/realtimelive/session.go`：PCM绕过Opus编解码，ASR用真实发送PCM重采样，去除PCM不需要的Opus补尾。原取消、credit和drain流程保持。

没有采用额外转码代理。测试夹具以[`.go.txt`](../../tools/c3-pcm-assessment/pcm_trial_test.go.txt)保存，仅复制进隔离副本后编译。复制后的原型文件SHA256：

| 文件 | SHA256 |
|---|---|
| protocol.go | 6087e4782a5371afa816ea2b54d4ccbd6fbe3142493066f608f44fe2672fee1b |
| session.go | d4f4ce77860b24b4eeb24be4354c128216a2aa31e9677c6df4bf6e5c8c6d31d3 |
| xiaozhi_entry.go | d76bc2087c1a2b0636e291ff87cd5edaddb8c06eb624b5140d6e04521974be3a |
| xiaozhi_wire.go | 4da69133276f19a35baf148efe4545e32ff15afa4ab23ba080f67f45d262716c |

脚本拒绝Git checkout和缺少marker的目录。所有替换锚点成功后才写文件。云端另验重复应用拒绝且四文件哈希不变、Git checkout拒绝，两项通过，结果留在`guard-result.json`。

最终工具在Mac与云端逐文件SHA256一致：`apply_trial.py`为`a36a6a764a01509300a6e7da43a420f9d4df625edcfbb175c648e4d3f05f5fd2`，最终测试夹具为`e445e766560f3d646a3953c317a090ba58da57c792eff82a0337c935b229a7bd`。

## RED、GREEN和兼容回归

最初未修改基线时，有效PCM协商及后续会话测试均因格式不支持失败，记录`red.log`。最后又用最终测试夹具在独立未修改基线运行协商测试：`TestPCMTrialNegotiation/valid`因服务端拒绝而失败，其余6个错误格式/能力用例通过，记录`final-red.jsonl`。编译错误的早期测试草稿不计RED。

最终云端Go1.27.3、libopus1.4，沿用已有依赖；差分测试通过任务目录内的`python -> /usr/bin/python3`运行，不安装Mac运行依赖：

| 检查 | 结果 | 私有记录 |
|---|---|---|
| PCM聚焦 | 7主测试/19含子用例pass，0.140s | focused.jsonl |
| `go test ./...` | 373主测试pass、2 skip；含子用例858 pass，13个有测试包通过 | all-packages.jsonl |
| PCM `-race` | 7主测试/19含子用例pass，1.184s | race.jsonl |

原有未变包可命中Go缓存；最终PCM相关包为实际重新执行。测试证明的边界：

1. PCM仅version1、mono16k/60ms、明确`pcm_s16le_v1`能力可协商，旧共享解析器拒绝PCM，9种原Opus version/frame组合保持。
2. 输入1920B、输出2880B严格检查，错误长度拒绝；不自动猜格式。PCM输入/输出逐字节保持，未调用Opus改变波形。
3. 未listen的音频不进入供应商业务输入，唤醒预算仍有上限；实际Session.Run、停止清理和播放期上行通过回环验证。
4. 5帧输出仍是7200个样本窗口；共同interrupt/drain、迟到旧包禁止、越界回报拒绝、有效i2s_dma回报和下一轮隔离通过。回报在此由测试终端模拟，不能写成真实C3 DMA验收。
5. 发送5帧后ASR取得4800个16k样本，未误把PCM当Opus。1、1439、1440、1441、2880样本及模型事件跨块输出均保持原数据，只补足最后1440样本边界，没有Opus专用312样本尾部。

## C3资源预算：有价值，但不能据此承诺余量

重新读取10月31日已保留的片上探针原始统计，同一FD mode5/原Audio配置：240个512样本块，7.68s音频预算；AEC4767698us（62.08%），Opus编码1583374us、解码1087738us，编解码合计34.78%，总96.86%。这些是实际C3合成探针数据，移除编解码后约62%的数值只是AEC下限，不含网络、SRC、配对、队列、显示和产品调度，不能当成PCM产品的CPU测量。

FD初始化后free237184B，codec加载时free193844B，差43340B，包含编解码状态及包工作区；同原codec下mode0/FD可用堆差22432B。不能将这两个差值直接加到真人样本min1412B上，就声称新固件有确定内存余量：分配峰值、生命周期和PCM缓存都不同。

### 必须按字节限制PCM队列

固定上游产品代码的`MAX_DECODE_PACKETS_IN_QUEUE=1200/60=20`，`MAX_SEND_PACKETS_IN_QUEUE=2400/60=40`。直接保留包数并改PCM，仅下行20×2880=57600B、上行40×1920=76800B，合计134400B，尚不含播放/组帧/正在发送/网络缓冲。这足以抵消预期收益。

下一阶段的设计预算建议：下行5个可复用1440样本槽，共14400B，覆盖共同7200样本credit窗口，应用接收/待播阶段交接同一槽，禁止重复保存一份解码后PCM；上行2个排队槽加1个正在发送槽，共5760B。另计WebSocket接收/解析缓冲、现有DMA、MR/SRC/AEC和系统分配。输入池用尽应明确结束故障会话并撤销代次，不无限排队或静默制造输入断裂。以上为预算方案，尚无终端实现或真机验证。

### 本地提示音仍会持有Opus

实际`AudioService::Initialize`会无条件创建Opus编码器和解码器；`PlaySound`解析Ogg并进入同一解码队列。仅改变网络格式不会释放这些状态，也不能在网络错误时突然分配解码器而不计峰值。

建议在C3专用构建中将所有可达本地提示音转换为24k单声道PCM，按小帧从Flash流式播放，避免把整段放入RAM。已核验的success/exclamation/zh-CN err_reg三个例子共5.76s，对应276480B PCM载荷，属于Flash预算；仍须完整列出全部可达提示音和资源分区大小，不能只转换三个例子或跳过提示音。其他variant保留原格式。

## 下一阶段门槛

云端评估支持继续该方向，下一阶段需要独立的C3固件设计与实现：固定字节池、无Opus常驻状态/任务的媒体路径、流式本地提示音、完整FD处理与有界声学统计。复用现有实际DMA记账、成对参考和capture代次；不调整服务端VAD来掩盖回声。

准备可安装候选前，要验证真实产品同时承载Wi-Fi/SRC/AEC/PCM的CPU/队列峰值、最小堆及最大连续块、任务栈和启动失败回退。建议至少保留16KiB最小内部堆、8KiB最大连续块，并以实际网络负载不积压、处理时限有余量为门槛；这些是拟定验收预算，尚无通过数据。声学统计只保存能量/峰值/削顶/时长/参考错误等数字，不保存录音。

本阶段不再让用户重复相同问候测试，也不以合成/回环替代真人扬声器无自触发与双讲验收。

收尾再核验正式Go PID4139298、8000 gateway PID4139310，均active/NRestarts0；18040仍build09d22e6、ready/memory_ready=true、active_sessions0/max2。仓库`internal/`、`firmware/`、go.mod/go.sum均无本阶段产品改动，评估没有部署到这些进程。

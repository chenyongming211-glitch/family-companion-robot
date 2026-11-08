# 无声表情选择器验证

这是 Task 5 的第一阶段工具：对预注册的 20 个合成场景测量**生产选择器的语义结果、请求数与耗时**。调用入口只有 `realtimelive.SelectExpression`，使用其固定 `ExpressionSelectionBudget`（2000ms），不实现另一套选择器、不重试、不调整截止时间或可接受集合。

本工具不播放音频、不连接设备，不测量固件 DMA 或屏幕生效窗口。所有汇总明确标记 `playback_window=not_measured`、`physical_status=not_measured`。配对语音开关对照和实机验收属于后续独立步骤；本工具保留并校验场景中的 `fault_cases`、语音场景与固定 `off,on,on,off` 顺序，但不执行这些步骤。强制故障通过 fake 和已有生产协议测试覆盖，不计为真实模型正常试次成绩。当前产品固件范围仅为微雪和 M5 的 ESP32-S3。

## 云端运行

所有 Go 测试、构建与实际模型调用都在云端私有验证目录执行。Mac 仅修改代码和操作 Git，不复制受保护配置或运行结果。不要提交配置、运行 JSON、日志、音频或生成二进制。

从仓库根目录执行，默认只读取并校验合成场景，输出数量、SHA256 和计划；即使传入 `--config` 或 `--out`，没有 `--run` 也不会读取配置、连接模型或创建输出目录：

```sh
/opt/family-robot/toolchains/go1.27.3/bin/go run ./tools/face-expression-check
```

真实模型验证应在工具与测试审查、提交完成后单独执行。下列参数中的配置和输出路径均为占位符；使用已有的受保护云端配置和**全新**的私有输出目录：

```sh
/opt/family-robot/toolchains/go1.27.3/bin/go run ./tools/face-expression-check \
  --run \
  --config /absolute/private/protected.env \
  --out /absolute/private/new-expression-run \
  --source-sha EXACT_40_HEX_COMMIT \
  --version face-expression-check-v1 \
  --run-label selector-01
```

`--fixture` 可显式指定预注册场景路径，默认是 `docs/verification/fixtures/2026-11-08-expression-scenes.json`。原清单在首次真实模型调用前已提交；保留其原始内容、顺序和 SHA256。不要根据评分修改同一协议的场景、可接受集合或期限。`--source-sha` 可选，提供时必须恰好 40 位十六进制；未提供时记录 `source_status=not_provided`，不推断来源。`--version` 和 `--run-label` 仅允许最多 64 个字母、数字、点、下划线和连字符，首字符必须是字母或数字。

`--run` 必须同时提供配置和输出的绝对、规范路径；所有父目录应已存在且不得包含符号链接。配置必须为无组/其它用户访问权限的普通文件。拒绝已有输出目录、符号链接和 `..` 等路径别名，避免覆盖旧失败。新建目录权限为 `0700`，文件为 `0600`；配置加载失败仅输出固定原因码，不输出密钥、URL 查询参数、配置内容或原始错误。

## 结果与判定

正常执行按 E01–E20 顺序串行调用，每例恰好一次选择器入口。每例只有同时满足以下条件才通过：

- `reason` 恰为 `completed`；
- 情绪为 15 个规范值之一且在该例预注册集合内；
- 选择器报告的时间有效，报告总耗时和工具观察耗时均小于固定 2000ms；
- 实际 `request_count` 恰为 1，且父上下文未取消。

`neutral` 只有作为真实完成的合法选择才可能通过。超时、错误或取消回退的 `neutral` 即使在集合中仍算失败。任何必测例失败、未运行、取消或输出写入失败，最终退出码非零；完整成功和合法默认校验退出码为 0，参数/场景校验失败为 2。请检查最终退出码；已保存的前缀文件不代表完整成功。

输出目录在任何模型调用前保存：

- `fixture.json`：逐字保存的原始清单；
- `metadata.json`：场景 SHA256、来源提交（如提供）、工具版本/运行标签和固定协议；
- `summary-000.json`：包含全部 20 例的初始计划。

每次调用前新增 `E01-attempt.json` 等启动凭证和独立 `E01-selector/` 生产审计目录；完成后新增 `E01.json` 及 `summary-001.json` 等不可覆盖的前缀汇总。最终新增 `summary.json`。所有失败、默认原因、实际请求计数、耗时和已提供 usage 均保留；SIGINT/SIGTERM 会取消当前选择器，保存其结果，并在汇总中列出剩余 `not_run` 场景。不可捕获的终止仍保留已同步写入的启动凭证与完成前缀，不能把缺失最终汇总解释为通过。

汇总从逐例记录重新计算通过/失败/未运行数量、原因和超请求计数，并提供连接/总耗时的最小值、中位数、nearest-rank P95、最大值、平均值和样本数。只有实际提供的 usage 字段才累加；`usage_observed_cases` 和 `usage_metric_cases` 表明覆盖数量。缺失 usage 不当作已知零，也不换算费用。包含超时/回退的有效时间仍进入耗时汇总。

E19 的合成 `assistant_played` 明确映射为生产历史投影需要的 `assistant_spoken_prefix`，文本逐字保留；无历史场景传入 `{"recent_turns":[]}`。设备名称通过现有 `deviceprofile.NormalizeName` 合同处理，输入由 JSON 编码，不把名称或历史当指令。

## 无真实调用的测试

测试只在 CLI 边界注入 fake 选择器与配置加载器；不会访问真实配置或 provider。默认路径测试的这两个依赖均设置为一旦访问即 panic。

```sh
/opt/family-robot/toolchains/go1.27.3/bin/go test -race ./tools/face-expression-check -count=1
/opt/family-robot/toolchains/go1.27.3/bin/go test -race ./internal/realtimelive -run '^TestExpression' -count=1
/opt/family-robot/toolchains/go1.27.3/bin/go vet ./tools/face-expression-check
/opt/family-robot/toolchains/go1.27.3/bin/go build -o /absolute/private/face-expression-check ./tools/face-expression-check
```

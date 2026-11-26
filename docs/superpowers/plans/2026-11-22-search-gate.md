# 2026-11-22 反向搜索门控（已于 2026-11-24 退役，改为全模型自主决策）

> **最新状态（2026-11-25 规范更新）：**
> 遵循 **“先极简、只在极简兜不住的关键处加一点点复杂度”** 与 **“取消所有服务端静态意图词表，所有语义决策交由大模型”** 的原则，本文档中的三层关键词门控（`searchFreshWords` / `searchMetaRecallWords` / `noSearchGroups`）已于提交 `4809fa8` 彻底移除。
> 当前实现（`internal/realtimelive/search_gate.go`）：只要服务端配置开启 `--search`，`searchDecision` 直接放行（`default_on`），是否发起联网检索 100% 由千问大模型根据对话上下文自主决定。完整现行计划见 [2026-11-23-route-c-and-speaker-roadmap.md](2026-11-23-route-c-and-speaker-roadmap.md)。以下保留作历史背景对照。

## 背景

搜索从第一版起就是每轮开着的（`--search` 默认 1，`sessionSettings` 把 `enable_search` 一起下发），
搜不搜由模型自己决定。实测代价：

| 轮次 | 内容 | input_tokens | 首包 |
| --- | --- | ---: | ---: |
| 7 | 推荐吃饭的地方 | 9272 | 6148 ms |
| 6 | 杭州天气 | 8036 | 2033 ms |
| 其余 | 讲故事、寒暄、回忆 | 3500–4100 | 1086–1702 ms |

一个搜索轮多注入约 5k token，尾部长 1–5 秒。而它搜的那些轮里，
讲故事、问「你叫什么」、问「记不记得我要去哪」本来都不需要新信息——
`memory_selection` 已经负责回忆，`music` 负责放歌，模型自己会讲故事。

所以这件事的收益不是「让慢的变快」，是**让不该慢的轮不再走那条路**。

## 决策：反着写

正向规则（命中关键词才搜）失败的方式最糟：该搜没搜，模型拿旧数据答，语气还很确定。
反向规则（命中才**不**搜）失败的方式只是多搜一次，代价一秒。

三层判定，从上到下：

1. **强开**：命中新鲜信息词（天气/新闻/价格/推荐/附近/几点/航班…）→ 搜。这一层压过下面所有层。
2. **强关**：命中五类明确不需要新信息的意图 → 不搜。
   - `continuation` 接着讲 / 再讲一个 / 还有吗
   - `story` 讲故事 / 讲个笑话 / 唱首歌
   - `recall` 记得 / 我刚才说 / 上次说
   - `company` 你好 / 谢谢 / 陪我 / 累了 / 难过
   - `opinion` 你觉得 / 你怎么看
   - `device` 大点声 / 停一下 / 再说一遍
3. **默认开**：其余全部保持原行为。转写为空、或输入超过 40 字（关键词无法分类）也走这一层。

「谢谢，明天天气怎么样」这类混合句子由第 1 层救回来——它是天气问题，不是道谢。
部署侧 `--search=0` 关闭时，门控不会把它打开。

## 实现

- `internal/realtimelive/search_gate.go`：纯函数 `searchDecision(user, configured) (*bool, string)`，
  返回这一轮生效的设置与理由，`nil` 表示配置未表态、不得改动。
- `generateNative`：按 turn id 取当轮转写，复制 `cfg` 后只改 `Search`，
  并记 `search_gate{id, allowed, reason}`。
- 不改 `Native`、不改账本、不改播放链路；`Native` 拿到的仍是每轮一份 `Config`。

## 观测

`search_gate`：`{id, allowed, reason}`，reason ∈ `fresh_information` / `continuation` /
`story` / `recall` / `company` / `opinion` / `device` / `long_input` / `default_on` /
`no_transcript` / `disabled_by_config` / `unset`。

上线后核对：`allowed=false` 的轮次不应出现 `native_search` 且 `input_tokens` 落回 3500–4100；
`allowed=true` 的轮次与今天一致。

## 验收

1. 「讲个故事」「你好」「再讲一个」→ `allowed=false`，首包回到 1.1–1.7s，无 `native_search`。
2. 「明天杭州天气」「推荐吃饭的地方」→ `allowed=true`，行为与今天一致。
3. 「谢谢，明天天气怎么样」→ `allowed=true`。
4. 现场连续聊 10 轮：不该出现「拿旧数据答新问题」的问答（这是本改动唯一的真实风险）。

## 回滚

一个提交；把 `generateNative` 里那三行删掉即恢复每轮开搜索，不动部署。

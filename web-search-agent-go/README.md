# Web Search Agent（Go）

第一章 ReAct 循环（Reasoning + Acting）的搜索 Agent：模型先**思考**，再调用 `web_search`
**行动**，**观察**搜索结果后继续思考，直到综合出最终答案。运行时会逐步打印 ReAct 轨迹。

本目录是 Python 版 `chapter1/web-search-agent` 的 Go 重构。搜索能力来自 Moonshot 的官方
Formula：工具声明从 `GET /formulas/moonshot/web-search:latest/tools` 拉取并原样转发，
搜索由 `POST /formulas/moonshot/web-search:latest/fibers` 执行。

## 快速开始

```bash
cp web-search-agent-go/env.example .env   # 填入 MOONSHOT_API_KEY

# 离线演示 ReAct 循环：零 API Key、零网络
go run ./web-search-agent-go/cmd/agentcli -provider offline-demo

# 单次问答
go run ./web-search-agent-go/cmd/agentcli "2024 诺贝尔物理学奖得主是谁？"

# 交互模式（每个问题相互独立，没有跨问题上下文）
go run ./web-search-agent-go/cmd/agentcli -output session.jsonl

# 实验 1-2：跑真实 API 并写出可复核的证据
go run ./web-search-agent-go/cmd/experiment12
```

主要选项：`-model`、`-max-steps`、`-base-url`、`-api-key`、`-output` / `-o`、`-quiet`、
`-allow-no-search`；`experiment12` 另有 `-attempts`、`-timeout`、`-output-dir`、`-as-of`。

## 关于轨迹里的「👀 观察」

**观察步骤展示的是 provider 返回的加密载荷，不是可读的搜索结果。** 归档的真实运行里
15 次搜索全部只返回 `context.encrypted_output`，只有模型能解密它。这是 Moonshot Formula
的既定行为，不是本实现的缺陷。因此轨迹的教学价值集中在 **thought（模型怎么想）** 与
**action（模型决定搜什么）** 两类步骤上：它们如实反映了 ReAct 的「想 → 做」。

## 代码结构

```
web-search-agent-go/
├── cmd/agentcli/          # 交互 / 单次 / 离线演示
├── cmd/experiment12/      # 实验 1-2 与证据落盘
└── internal/
    ├── config/            # 凭据、端点、超时解析
    ├── contract/          # 运行时与验收共享的判定规则（无 HTTP）
    ├── moonshot/          # 协议层：唯一接触 HTTP 的包
    ├── react/             # ReAct 循环、轨迹、离线演示
    ├── session/           # 把上面几层装配成「问一个问题」
    ├── acceptance/        # 18 项验收断言（纯函数）
    └── evidence/          # 证据序列化、SHA-256、凭证扫描
```

`acceptance` 与 `evidence` 都不依赖协议层的 HTTP 代码：验收是纯函数，既能校验实时运行，
也能回放归档证据。

## 验收

```bash
go vet ./web-search-agent-go/...
gofmt -l web-search-agent-go
go test ./web-search-agent-go/...
go test -race ./web-search-agent-go/...
```

主验收门是**跨语言黄金回放**：`testdata/golden/latest.json` 是 Python 版留下的一次真实运行
（21 次 HTTP 调用、5 轮迭代、15 个 Formula Fiber）。`TestGoldenReplay` 用 Go 的校验器重算
18 项断言，逐项与 Python 当时记录的结论比对，并核对 fiber 凭据与 token 汇总
（`prompt 54021 / completion 4102 / total 58123 / cached 29952 / reasoning 1698`）。
**不存在「合理差异」**：任何一项不一致都说明 Go 版的解析或判定有 bug。

## 与 Python 版的已知行为差异

| 差异 | 说明 |
|---|---|
| **同轮工具调用并行执行** | Python 在一轮内串行跑完所有 `web_search`。Go 用 `errgroup` 并行（默认上限 5），结果按原顺序回填历史。用归档的真实耗时实测：fiber 段由 81.8 秒降到约 28 秒 |
| **轨迹里 action / observation 由交替变为分组** | 并行执行下「一问一答」式的交替没有意义，改为先发出该轮全部 action、再发出全部 observation。18 项断言只看步骤类型、`tool` 字段与 `iteration` 分组，结论不受影响，且有测试固化这一点 |
| **无搜索模式默认拒绝启动** | 缺少 Moonshot 凭据时，Python 会静默改走 OpenRouter，「搜索 Agent」悄悄退化为凭记忆答题。Go 版默认拒绝启动，需显式 `-allow-no-search`，且降级时在 stderr 打醒目警告 |
| **官方来源判定收紧** | Python 用裸子串匹配 `("go.id", "polri.go.id", "mkri.id")`：`polri.go.id` 是 `go.id` 的冗余子串，裸 `go.id` 会误命中 `lego.idea.com`，而最贴题的 `timor-leste.gov.tl` 反而不在名单里。Go 版解析答案里的主机名后按域名后缀匹配 |
| **检索基准日期成为参数** | 事实断言与基准日期绑定，现由 `-as-of` 给出（默认 `2026-07-30`），问题文案与断言一起跟着走 |
| **交互模式的 `-output` 改为 JSONL 追加** | Python 每问覆盖同一个文件，一场会话结束只剩最后一条。单次模式仍写单个 JSON 文件 |
| **证据落盘前强制凭证扫描** | Python 把 `credential_value_recorded: false` 硬编码进证据却从不核实。Go 版在写入前扫描全部已配置密钥的值与 `Authorization` / `Bearer` 痕迹，命中即拒绝写入且不创建文件，该字段由扫描结果推导 |
| **`created_at` 时间格式** | Python 的 `isoformat()` 产生 `...+00:00`，Go 的 RFC3339Nano 产生 `...Z`。证据目录名 `20060102T150405Z` 两边一致 |
| **`host` 字段记录 Go 运行时** | Python 记 `python` 版本，Go 记 `go` 版本；`platform` 为 `GOOS/GOARCH` |
| **删除了 `clear` 命令** | Python 的 `clear_history()` 是死代码（每次提问入口已重置状态），却暴露成交互模式的 `clear` 命令。Go 版不提供，改为在开场文案里说明「每个问题相互独立」 |
| **不提供手输 API Key 的入口** | Python 的 `quickstart.py` 用 `input()` 读 Key，明文回显到终端与 shell 历史。Go 版只从环境变量 / `.env` / `-api-key` 读取 |
| **轨迹渲染的参数 JSON 无键值空格** | Python 的 `json.dumps` 默认 `": "` 分隔，Go 是紧凑形式。纯展示差异，不影响任何断言 |

## 实现上必须保留的几处约定

- **工具声明原样透传。** `ChatRequest.Tools` 是 `[]json.RawMessage`，从不解码再编码——
  provider schema 里任何未声明的字段都不能被静默丢弃。
- **Fiber 请求携带模型生成的原始 `arguments` 字符串**，而不是解析后重新序列化的副本。
- **请求侧 `Message` 与响应侧 `ChoiceMessage` 是两个类型。** 回传历史必须经
  `ToAssistantMessage()` 转换，`reasoning_content` 因此在结构上不可能被带回去——
  带着它回传 Moonshot 会返回 400 `tokenization failed`。
- **`finish_reason == "length"` 与「没拿到信息」严格区分**：前者返回已生成的部分并标注截断，
  后者才是「无法获取足够的信息」。
- **`sequential_search_rounds_observed` 数的是搜索分布在几个不同迭代**，不是搜索总次数；
  「一轮并行扇出 10 次」必须判定失败。
- **失败如实记录。** 失败的尝试连完整 API 轨迹一起写入 `transient_failed_attempts`。

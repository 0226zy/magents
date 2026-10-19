# agent-with-event-trigger-go

事件驱动 Agent 的 Go 实现，对应 `ai-agent-book/chapter6/agent-with-event-trigger`。

Agent 不再是「用户提问 → 回答」的一问一答，而是**常驻等待事件**：网页消息、IM 消息、
邮件回复、GitHub PR 更新、定时器、文件变更、用户超时、进程超时、系统告警，九种事件
统一建模成同一条消息流，共享同一份会话历史。

文档：[OVERVIEW.md](OVERVIEW.md)（功能、设计用途、实验目标）、
[DESIGN.md](DESIGN.md)（架构分层）、[DOMAIN_DESIGN.md](DOMAIN_DESIGN.md)（领域代码）。
第一次接触这个项目，从 OVERVIEW 开始。

---

## 快速开始

无需任何 API Key，先看一遍「触发 → 唤醒 → 处理」的闭环：

```bash
go run ./cmd/eventloop -mock -duration 12
```

另开一个终端往监听目录写文件，会立刻触发 `file_change` 事件：

```bash
echo "deploy failed at 3am" > watched_dir/alert.txt
```

接真实模型：复制 `env.example` 为 `.env` 填好 Key，然后

```bash
go run ./cmd/server                      # 终端 1：启动事件服务
go run ./cmd/client -mode test           # 终端 2：依次投递六类事件
go run ./cmd/client -mode interactive    # 或者手动输入
```

---

## 四个可执行程序

| 命令 | 作用 | 常用参数 |
|---|---|---|
| `cmd/server` | HTTP 事件服务，13 个端点 | `-config` `-host` `-port` `-provider` `-model` |
| `cmd/client` | 事件客户端 | `-mode test\|interactive` `-message` `-event-type` |
| `cmd/eventloop` | 单进程事件循环演示 | `-mock` `-trigger timer\|recurring\|file\|all` `-duration` |
| `cmd/experiment61` | 实验 6-1：真实邮箱工作流 | `-preflight-only` `-campaign-id` `-poll-timeout` |

---

## HTTP 接口

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/` | 服务信息与端点索引 |
| GET | `/health` | 健康检查（不会被正在跑的事件阻塞） |
| POST | `/event` | 投递事件，同步返回处理结果 |
| GET | `/agent/status` | provider / model / 计数 / 工作目录 / MCP 状态 |
| POST | `/agent/reset` | 清空历史、TODO、计数、进程表 |
| GET | `/mcp/status` | MCP 加载状态与工具清单 |
| POST | `/mcp/reload` | 后台重新加载；加载中返回 409 |
| POST | `/process/register` | 注册后台进程（`name` / `process_name` 都认） |
| POST | `/process/unregister` | 注销后台进程 |
| POST | `/monitoring/start` | 启动超时监控 |
| POST | `/monitoring/stop` | 停止超时监控 |
| GET | `/monitoring/status` | 监控阈值与运行状态 |

投递一个事件：

```bash
curl -s localhost:8000/event -H 'Content-Type: application/json' -d '{
  "event_type": "email_reply",
  "content": "Thanks for the report! Can you also check the disk usage?",
  "metadata": {"from": "bob@example.com", "subject": "Re: System Report"}
}'
```

错误码：400（请求体非法 / 未知事件类型）、404（未知路径）、405（方法不匹配）、
409（MCP 正在加载）、500（内部错误）。

---

## 配置

优先级：**命令行 flag > 环境变量 > `.env` > `config.yaml` > 内置默认值**。
完整字段见 [`config.yaml`](config.yaml)，凭据见 [`env.example`](env.example)。

支持 dashscope / siliconflow / doubao / kimi / openrouter 五家 OpenAI 兼容端点。
所选 provider 没有 Key 但设置了 `OPENROUTER_API_KEY` 时整体回落到 OpenRouter。

---

## 实验 6-1：真实邮箱事件工作流

以真实邮箱为事件源，向自己投递三封带标记的邮件，轮询收件箱，按 provider 时间排 FIFO
队列逐封处理：

- **会议邀请** → 查真实日历是否冲突 → 据此写出接受或婉拒的回复草稿
- **客户投诉** → 抽取订单号 → 投递 fsync 落盘的高优通知
- **营销邮件** → 归档 → **回读校验**归档确实生效

```bash
go run ./cmd/experiment61 -preflight-only    # 先验凭据与账号
go run ./cmd/experiment61                    # 完整运行
```

证据写到 `validation/<campaign-id>/`：`protocol.json`、`summary.json`、
`manifest.json`、`artifacts/`。三种终态：

| 终态 | 含义 |
|---|---|
| `passed` | 10 项验收门全过 |
| `failed` | 实验跑了但有门没过 |
| `blocked` | 凭据缺失或 401，实验没跑起来 |

缺凭据时**不会降级跑假数据**，而是停在 `blocked` 并照常写出证据目录——
失败的运行同样是证据。所有回执经过脱敏与 SHA-256 哈希，写盘前还要过一次凭据形状扫描，
扫到就拒绝落盘。

---

## 相对 Python 原版的修正

实现过程中发现并修掉了原版的 12 处缺陷，完整清单见 [DESIGN.md](DESIGN.md) 第二节。
影响最大的几条：

| 原版行为 | 本实现 |
|---|---|
| 客户端调 `/monitoring/*`，服务端没有这组端点 | 补齐三个端点，并真正实现超时监控 |
| `user_timeout` / `process_timeout` 定义了却从不产生 | `application/monitor` 周期检查并派发 |
| 服务端收 `name`、客户端发 `process_name`，注册必然失败 | 两种拼写都接受 |
| `/event` 丢掉 `event_id`，并把累计工具调用数当成单次值 | 都返回，且区分单次与累计 |
| 把 `reasoning_content` 回传给 provider，导致 400 | 请求与响应用不同类型，结构上不可能回传 |
| 每次工具调用都重启一次 MCP 子进程 | 常驻会话 + 惰性重连 |
| `code_interpreter` 在进程内 `exec` | 独立子进程 + 超时 |
| TODO 批量更新遇非法状态会半更新 | 先全量校验，整批要么全成要么全不动 |
| 轨迹直接覆盖写，崩溃留半截 JSON | 先写 `.tmp` 再 `os.Rename` 原子替换 |

---

## 测试

```bash
go build ./... && go vet ./... && go test -race ./... && gofmt -l .
```

覆盖：领域纯函数（分类、脱敏、验收门、TODO 不变量）、Agent 工具循环（假模型）、
超时监控（受控时钟）、事件循环（内存触发器）、LLM 与 Unipile 协议（`httptest`）、
MCP stdio 握手（真实子进程）、全部 HTTP 端点（`httptest.Server`）。

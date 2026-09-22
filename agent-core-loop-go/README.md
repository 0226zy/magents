# agent-core-loop-go

书中 2.2.4 节「用代码实现 Agent 的核心循环」的 Go 实现：一个 while 循环——
携带完整消息列表与工具声明调用模型，把 assistant 回复（文本或 tool_calls）追加进消息列表；
若有工具调用则逐个执行、把 tool 结果回填，再回到循环顶部重新调用模型，直到模型给出最终回答。

## DDD 分层

```
agent-core-loop-go/
├── cmd/agent/main.go                  # 进程入口：装配 + 启动
├── internal/
│   ├── interfaces/handler.go          # 对外协议适配（CLI）：解析参数、转发、打印结果
│   ├── application/service/           # 应用层：用例编排
│   │   ├── agent_service.go           #   AgentService.Execute() —— 核心 while 循环
│   │   └── agent_service_test.go      #   fake LLM/Tools 离线验证循环与 max_iterations
│   ├── domain/                        # 领域层：零外部依赖
│   │   ├── entity/message_entity.go   #   协议 DTO：Message / ToolCall / ToolDefinition / ChatResponse
│   │   ├── port/llm_port.go           #   端口：ChatModelPort / ToolPort / TrajectoryPort / RawLogPort
│   │   └── derr/errors.go             #   哨兵错误：ErrMaxIterations
│   ├── infrastructure/                # 基础设施层：端口实现
│   │   ├── config/config.go           #   YAML 配置加载
│   │   └── adapter/                   #   Moonshot 客户端 / 工具注册表 / 轨迹日志
│   └── shared/deps/service_context.go # 依赖注入容器（组合根）
└── config.yaml                        # 模型 SDK 配置
```

依赖方向：`interfaces → application → domain`，`infrastructure → domain`，
组合根在 `shared/deps` 完成注入。

## 配置（config.yaml）

`api_key`（留空读环境变量 `MOONSHOT_API_KEY` / `KIMI_API_KEY`）、`base_url`、`model`、
`timeout_seconds`、`max_iterations`（循环上限，防死循环）、`log_dir`（轨迹日志目录）、
`enable_raw_log`（是否记录模型原始响应到单独的 raw 文件，默认 `true`）、`system_prompt`。

## 运行

```bash
cd agent-core-loop-go
go run ./cmd/agent "What's the current time and weather in Vancouver?"
go test ./internal/application/     # 离线验证核心循环逻辑
```

每次运行生成一对日志文件（同一时间戳，便于配对）：

- `logs/agent-YYYYMMDD-HHMMSS.log` —— 轨迹日志：终端 + 文件，含每轮发给模型的完整 messages（单行 JSON）
- `logs/agent-YYYYMMDD-HHMMSS-raw.log` —— 模型原始响应：只写文件、不上终端，每轮一行 verbatim JSON

终端与轨迹文件内容一致，例如：

```
[16:44:24.434] === 用户问题: What's the current time and weather in Vancouver?
[16:44:24.434] --- 第 1/8 轮: 调用模型（messages=2）
[16:44:24.434] >>> 第 1 轮发给模型的完整 messages: [{"role":"system","content":"You are a helpful assistant. ..."},{"role":"user","content":"What's the current time and weather in Vancouver?"}]
[16:44:24.434] >>> 模型请求工具: get_current_time({"timezone":"America/Vancouver"})
[16:44:24.434] <<< 工具结果: {"datetime":"2025-09-13T05:18:47", "day_of_week":"Saturday"}
[16:44:24.434] --- 第 2/8 轮: 调用模型（messages=5）
[16:44:24.434] >>> 第 2 轮发给模型的完整 messages: [{"role":"system",...},{"role":"user",...},{"role":"assistant","tool_calls":[...]},{"role":"tool","tool_call_id":"call_1",...},...]  ← 含历史 assistant 与 tool 结果
[16:44:24.434] <<< 模型回复: In Vancouver, it's Saturday ...
[16:44:24.434] === 循环结束: finish_reason=stop, tokens(...)
```

发给模型的完整 messages 与工具声明均压缩为单行 JSON，便于逐行检索日志。

注：`get_current_time` 与 `get_weather` 均为桩实现，不接任何真实 API，调用一律返回固定默认值。

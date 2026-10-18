# magents 架构规范（Go DDD 分层）

本仓库所有 Go 子项目统一遵循 go-ddd 分层规范（参考 `cfm_skills/go-ddd`）。
一句话：**依赖指向领域，领域不依赖任何人。**

```
        ┌─────────────┐
        │ interfaces  │  对外协议适配（CLI / 批处理入口）
        └──────┬──────┘
               ↓
        ┌─────────────┐
        │ application │  用例编排（service）
        └──────┬──────┘
               ↓
        ┌─────────────┐
        │   domain    │  领域模型 + 契约（entity / port / derr，零外部依赖）
        └──────┬──────┘
               ↑
        ┌──────┴──────┐
        │infrastructure│ 技术实现（config / adapter）
        └─────────────┘
```

## 目录结构

```
<project>/
├── cmd/<service>/main.go              # 进程入口：只装配，不含业务
├── internal/
│   ├── interfaces/                    # 对外协议适配：解析参数、转发、输出
│   ├── application/service/           # 应用服务：Execute 用例编排
│   ├── domain/
│   │   ├── entity/                    # 协议 DTO / 领域对象（纯 struct）
│   │   ├── port/                      # 下游能力端口接口
│   │   └── derr/                      # 哨兵错误（errors.Is 判断）
│   ├── infrastructure/
│   │   ├── config/                    # 配置加载
│   │   └── adapter/                   # 端口实现（HTTP 客户端 / 工具 / 日志）
│   └── shared/deps/                   # 依赖注入容器（组合根）
```

## 硬约束

1. `domain` 零外部依赖：只允许标准库与本模块其他 `domain` 子包。禁止 import
   `infrastructure` / `interfaces` / ORM / RPC stub。
2. 依赖只能向内：`interfaces → application → domain`，`infrastructure → domain`。
   `application` 禁止 import `infrastructure`（测试文件打桩除外）。
3. 接口定义在 `domain/port`，实现放 `infrastructure/adapter`，
   由 `shared/deps` 在组合根注入。
4. 错误用哨兵错误（`domain/derr`）+ `%w` 包装，跨层判断用 `errors.Is`，
   绝不用字符串比较。
5. 文件名 snake_case，类型名 CamelCase；一个用例一个应用服务文件。

## 各项目落位

| 项目 | 入口 | 用例 | 端口 | 适配器 |
|---|---|---|---|---|
| kimi-chat-go | cmd/kimi-chat | ChatService.Execute | ChatModelPort | MoonshotClient |
| agent-core-loop-go | cmd/agent | AgentService.Execute | ChatModelPort / ToolPort / TrajectoryPort / RawLogPort | MoonshotClient / ToolRegistry / Trajectory+RawFile |
| web-search-agent-go | cmd/agentcli, cmd/experiment12 | react（ReAct 循环）、session（提问用例） | ChatCaller / FormulaRunner | moonshot（chat+formula）|
| dsh-go | cmd/dshd | agent.Loop.RunTurn（ReAct 循环）、session.Manager（resume/fork/replay/search） | LLMPort / Tool / SessionStorePort / Hook / StatusBarPort / SessionRuntime | openai 兼容适配器 / storejsonl / tools（bash·文件·搜索）/ policy / statusbar |

## 提交前自检

```bash
go build ./... && go vet ./... && go test ./...
grep -rn 'net/http|grpc|gorm|sqlx' */internal/domain/          # 必须无输出
grep -rn 'internal/infrastructure|internal/interfaces' \
    */internal/domain/ */internal/application/ | grep -v _test.go   # 必须无输出
gofmt -l .                                                     # 必须无输出
```

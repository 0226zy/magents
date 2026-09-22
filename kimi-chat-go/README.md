# kimi-chat-go

对 Kimi（Moonshot）模型 API 的最小调用示例（Go），对应书中 2.2 节
「Agent 如何调用大模型：理解 API 的上下文结构」。

## 结构

```
kimi-chat-go/
├── cmd/kimi-chat/main.go              # 进程入口：装配 + 启动
├── internal/
│   ├── interfaces/handler.go          # CLI 适配：解析参数、转发、打印结果
│   ├── application/service/           # 应用层：单次问答用例编排
│   ├── domain/                        # 领域层：协议 DTO + ChatModelPort 端口 + 哨兵错误
│   ├── infrastructure/                # 配置加载 + Moonshot 适配器
│   └── shared/deps/                   # 依赖注入容器（组合根）
└── config.yaml                        # 模型 SDK 相关配置（密钥、端点、模型名、采样参数、超时、系统提示词）
```

## 配置

编辑 `config.yaml`，填入从 https://platform.moonshot.cn/ 申请的 API Key；
也可以把 `api_key` 留空，改用环境变量 `MOONSHOT_API_KEY`（或旧名 `KIMI_API_KEY`）。
注意：环境变量里的 Key 需要是 Moonshot 开放平台签发的，kimi-code 等内部网关的 Key 会返回 401。

## 运行

```bash
go run ./cmd/kimi-chat "Hello, who are you?"
go run ./cmd/kimi-chat -config config.yaml "用一句话解释什么是 Agent"

# 或编译后运行
go build -o kimi-chat ./cmd/kimi-chat && ./kimi-chat "你好"
```

输出助手回复，并在 stderr 打印模型名、finish_reason 与 token 用量。

## 请求体示例

即书中描述的上下文结构：

```json
{
  "model": "kimi-k3",
  "messages": [
    {"role": "system", "content": "You are a helpful coding assistant. Follow user instructions."},
    {"role": "user", "content": "Hello, who are you?"}
  ],
  "temperature": 0.3,
  "max_tokens": 2048
}
```

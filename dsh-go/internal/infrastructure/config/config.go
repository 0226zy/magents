// Package config 负责从 YAML 文件加载服务端配置（dsh profile 的等价物：
// 启用哪些插件、模型参数、预算与熔断阈值都由配置决定）。
package config

import (
	"fmt"
	"net"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 对应 dsh.yaml。
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Model    ModelConfig    `yaml:"model"` // legacy：models 文件缺失时的种子/兜底
	Agent    AgentConfig    `yaml:"agent"`
	Tools    ToolsConfig    `yaml:"tools"`
	Storage  StorageConfig  `yaml:"storage"`
	MCP      MCPConfig      `yaml:"mcp"`      // MCP 工具生态（空 = 不启用）
	Deploy   DeployConfig   `yaml:"deploy"`   // 部署目标（空 = 不注册 deploy 工具）
	Security SecurityConfig `yaml:"security"` // 部署安全边界（工作区白名单等）
	// ModelsFile 是 LLM 模型信息资源（LLMInfoRepo）的本地文件路径；
	// 后续切换 DB 仓储时本字段由 db 配置替代，上层不变。
	ModelsFile string `yaml:"models_file"`
	// SkillsDir 是工程技能目录（agentskills.io 兼容，每子目录一份 SKILL.md）；
	// 空或目录不存在 = 不启用技能能力。
	SkillsDir string `yaml:"skills_dir"`
}

type ServerConfig struct {
	Listen string `yaml:"listen"` // 例：127.0.0.1:3080
	// AuthToken 为空表示不启用鉴权（仅适合本机开发）；服务端部署必须配置。
	AuthToken string `yaml:"auth_token"`
}

type ModelConfig struct {
	// Provider 是插件名（注册表里 LLM 插件的 key），不是厂商名。
	Provider    string  `yaml:"provider"`
	BaseURL     string  `yaml:"base_url"`
	APIKey      string  `yaml:"api_key"` // 建议留空走环境变量
	Model       string  `yaml:"model"`
	Temperature float64 `yaml:"temperature"`
	MaxTokens   int     `yaml:"max_tokens"`
	// TimeoutSeconds 是单次模型调用的超时（活性看门狗：静默卡死即杀流重试）。
	TimeoutSeconds int `yaml:"timeout_seconds"`
}

type AgentConfig struct {
	MaxIterations int    `yaml:"max_iterations"` // 全局终止条件之一
	TokenBudget   int    `yaml:"token_budget"`   // 全局终止条件之二
	DefaultMode   string `yaml:"default_mode"`   // suggest / auto-edit / full-auto
	// LivelockRepeat：相同「工具名+参数」指纹连续出现 N 次即判定无进展。
	LivelockRepeat int `yaml:"livelock_repeat"`
	// MaxRetries：可重试错误（限流/过载/超时）的静默重试上限，指数退避+抖动。
	MaxRetries int `yaml:"max_retries"`
	// CircuitBreakerFailures：任一恢复路径连续失败达到阈值即熔断。
	CircuitBreakerFailures int `yaml:"circuit_breaker_failures"`
	// ApprovalTimeoutSeconds：高风险操作审批等待人类裁决的超时。
	ApprovalTimeoutSeconds int `yaml:"approval_timeout_seconds"`
	// SystemPromptFile：SOUL 层文件路径（身份、行为准则、忠诚度守则）。
	SystemPromptFile string `yaml:"system_prompt_file"`
	// SubagentMaxIterations：子代理（task 工具）的迭代上限，应显著小于主循环。
	SubagentMaxIterations int `yaml:"subagent_max_iterations"`
	// ContextCompressAtPercent：会话累计 token 超过 token_budget 的该百分比时，
	// 投影到模型上下文的较早工具输出被压缩为「摘录 + spill 全文路径」（评审 P1）。
	// 0 = 禁用；事件日志本体永远完整，压缩只发生在投影层。
	ContextCompressAtPercent int `yaml:"context_compress_at_percent"`
	// ContextKeepRecent：压缩时保留 verbatim 的最近消息条数（工作集）。
	ContextKeepRecent int `yaml:"context_keep_recent"`
}

type ToolsConfig struct {
	BashTimeoutSeconds int `yaml:"bash_timeout_seconds"` // 单条 bash 命令超时（结构化超时错误）
	TestTimeoutSeconds int `yaml:"test_timeout_seconds"` // run_tests 缺省超时（测试通常比单条命令慢）
	MaxOutputBytes     int `yaml:"max_output_bytes"`     // 工具输出截断阈值（头尾保留，全文落盘）
	GrepMaxMatches     int `yaml:"grep_max_matches"`
	GlobMaxResults     int `yaml:"glob_max_results"`
}

type StorageConfig struct {
	DataDir string `yaml:"data_dir"` // 会话日志、spill 输出、审计原始报文的根目录
}

// MCPConfig 是 MCP（Model Context Protocol）工具生态的接入配置：
// 空 = 不启用（配置决定启用，对齐插件哲学）。
type MCPConfig struct {
	Servers []MCPServerConfig `yaml:"servers"`
}

// MCPServerConfig 描述一个 stdio 传输的 MCP server 子进程。
type MCPServerConfig struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Env     []string `yaml:"env"`
	// ReadOnly 声明该 server 的全部工具为只读（免审批）；仅在确认其不修改环境时开启。
	ReadOnly       bool `yaml:"read_only"`
	TimeoutSeconds int  `yaml:"timeout_seconds"` // 单次 RPC 超时（0 = 60）
}

// DeployConfig 是部署与运维层配置：空 = 不注册 deploy 工具（配置决定启用）。
type DeployConfig struct {
	Targets []DeployTargetConfig `yaml:"targets"`
}

// DeployTargetConfig 描述一个部署目标：命令模板固化，模型只能填参数。
// Command 中的参数占位符形如 {{image}}；参数值经 shell 转义后替换。
type DeployTargetConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Command     string `yaml:"command"`
}

// SecurityConfig 是部署安全边界（评审 P0-3/P0-4）。
type SecurityConfig struct {
	// AllowedCwdRoots 是会话工作区的白名单根目录：会话 cwd 必须落在其一之内
	// （空 = 不限制，仅适合本机开发）。多租户/服务端部署应配置，防止逃逸到工作区外。
	AllowedCwdRoots []string `yaml:"allowed_cwd_roots"`
}

// Load 从 YAML 文件读取配置，环境变量可覆盖密钥类字段。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件 %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s: %w", path, err)
	}

	// 密钥只从环境变量取，避免写进配置文件进入版本库
	if cfg.Model.APIKey == "" {
		for _, env := range []string{"MAGENTS_API_KEY", "DEEPSEEK_API_KEY", "MOONSHOT_API_KEY", "KIMI_API_KEY", "OPENAI_API_KEY"} {
			if v := os.Getenv(env); v != "" {
				cfg.Model.APIKey = v
				break
			}
		}
	}
	if v := os.Getenv("MAGENTS_AUTH_TOKEN"); v != "" {
		cfg.Server.AuthToken = v
	}

	// 默认值
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = "127.0.0.1:3080"
	}
	if cfg.Model.Provider == "" {
		cfg.Model.Provider = "openai-compat"
	}
	if cfg.Model.BaseURL == "" {
		cfg.Model.BaseURL = "https://api.deepseek.com/v1"
	}
	if cfg.ModelsFile == "" {
		cfg.ModelsFile = "models.yaml"
	}
	if cfg.SkillsDir == "" {
		cfg.SkillsDir = "skills"
	}
	if cfg.Model.MaxTokens <= 0 {
		cfg.Model.MaxTokens = 8192
	}
	if cfg.Model.TimeoutSeconds <= 0 {
		cfg.Model.TimeoutSeconds = 120
	}
	if cfg.Agent.MaxIterations <= 0 {
		cfg.Agent.MaxIterations = 50
	}
	if cfg.Agent.TokenBudget <= 0 {
		cfg.Agent.TokenBudget = 1_000_000
	}
	if cfg.Agent.DefaultMode == "" {
		cfg.Agent.DefaultMode = "auto-edit"
	}
	if cfg.Agent.LivelockRepeat <= 0 {
		cfg.Agent.LivelockRepeat = 3
	}
	if cfg.Agent.MaxRetries <= 0 {
		cfg.Agent.MaxRetries = 5
	}
	if cfg.Agent.CircuitBreakerFailures <= 0 {
		cfg.Agent.CircuitBreakerFailures = 3
	}
	if cfg.Agent.ApprovalTimeoutSeconds <= 0 {
		cfg.Agent.ApprovalTimeoutSeconds = 300
	}
	if cfg.Agent.SubagentMaxIterations <= 0 {
		cfg.Agent.SubagentMaxIterations = 25
	}
	if cfg.Agent.ContextCompressAtPercent < 0 || cfg.Agent.ContextCompressAtPercent > 100 {
		return nil, fmt.Errorf("agent.context_compress_at_percent 应在 0..100 之间（0 = 禁用），当前 %d", cfg.Agent.ContextCompressAtPercent)
	}
	// ContextCompressAtPercent 缺省 0 = 禁用；dsh.yaml 显式给出 70。
	// （yaml 缺键与显式 0 无法区分，选择尊重显式 0 的禁用语义。）
	if cfg.Agent.ContextKeepRecent <= 0 {
		cfg.Agent.ContextKeepRecent = 16
	}
	if cfg.Tools.BashTimeoutSeconds <= 0 {
		cfg.Tools.BashTimeoutSeconds = 120
	}
	if cfg.Tools.TestTimeoutSeconds <= 0 {
		cfg.Tools.TestTimeoutSeconds = 300
	}
	if cfg.Tools.MaxOutputBytes <= 0 {
		cfg.Tools.MaxOutputBytes = 30_000
	}
	if cfg.Tools.GrepMaxMatches <= 0 {
		cfg.Tools.GrepMaxMatches = 100
	}
	if cfg.Tools.GlobMaxResults <= 0 {
		cfg.Tools.GlobMaxResults = 200
	}
	if cfg.Storage.DataDir == "" {
		cfg.Storage.DataDir = "data"
	}
	return &cfg, nil
}

// CheckDeployment 部署前置检查（评审 P0-3）：非回环监听 + 空鉴权令牌 = 拒绝启动。
// 公网/多租户暴露必须启用鉴权；本机开发用回环地址即可免检。
func CheckDeployment(cfg *Config) error {
	host, _, err := net.SplitHostPort(cfg.Server.Listen)
	if err != nil {
		host = cfg.Server.Listen // 无端口形式按整串处理
	}
	if cfg.Server.AuthToken == "" && !isLoopbackHost(host) {
		return fmt.Errorf("监听地址 %q 不是回环地址且未配置 auth_token：公网/多租户暴露必须启用鉴权（server.auth_token 或 MAGENTS_AUTH_TOKEN），或把 server.listen 改为 127.0.0.1", cfg.Server.Listen)
	}
	return nil
}

// isLoopbackHost 判断监听主机是否回环（空 host = 监听全部接口，视为非回环）。
func isLoopbackHost(host string) bool {
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

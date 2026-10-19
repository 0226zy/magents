// Package config 负责加载运行配置。
// 优先级：命令行 flag > 环境变量 > .env > config.yaml > 内置默认值。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
)

// ServerConfig 是 HTTP 服务的监听配置。
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// HintsConfig 是 system hint 开关，字段与 entity.SystemHintConfig 对应。
// 用指针区分「未配置」与「显式 false」：未配置时沿用默认开启。
type HintsConfig struct {
	EnableTimestamps     *bool  `yaml:"enable_timestamps"`
	EnableToolCounter    *bool  `yaml:"enable_tool_counter"`
	EnableTodoList       *bool  `yaml:"enable_todo_list"`
	EnableDetailedErrors *bool  `yaml:"enable_detailed_errors"`
	EnableSystemState    *bool  `yaml:"enable_system_state"`
	SimulateTimeDelay    *bool  `yaml:"simulate_time_delay"`
	SaveTrajectory       *bool  `yaml:"save_trajectory"`
	TrajectoryFile       string `yaml:"trajectory_file"`
}

// MonitorConfig 是超时监控配置。
type MonitorConfig struct {
	Enabled               bool `yaml:"enabled"`
	IntervalSeconds       int  `yaml:"interval_seconds"`
	UserTimeoutSeconds    int  `yaml:"user_timeout_seconds"`
	ProcessTimeoutSeconds int  `yaml:"process_timeout_seconds"`
}

// MCPServerConfig 显式声明一个 MCP 服务器的启动方式。
//
// Python 版把路径硬编码成 ../collaboration-tools/src/main.py，在 chapter6 下根本不存在，
// 三个服务器全部静默加载失败却不报错。这里要求把命令与参数写进配置，加载结果逐个回报。
type MCPServerConfig struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Env     []string `yaml:"env"`
}

// MCPConfig 是 MCP 工具接入配置。
type MCPConfig struct {
	Enabled bool              `yaml:"enabled"`
	Servers []MCPServerConfig `yaml:"servers"`
}

// Config 是完整运行配置。
type Config struct {
	Provider       string        `yaml:"provider"`
	APIKey         string        `yaml:"api_key"`
	Model          string        `yaml:"model"`
	BaseURL        string        `yaml:"base_url"`
	Temperature    float64       `yaml:"temperature"`
	MaxTokens      int           `yaml:"max_tokens"`
	TimeoutSeconds int           `yaml:"timeout_seconds"`
	MaxIterations  int           `yaml:"max_iterations"`
	SystemPrompt   string        `yaml:"system_prompt"`
	Server         ServerConfig  `yaml:"server"`
	Hints          HintsConfig   `yaml:"hints"`
	Monitor        MonitorConfig `yaml:"monitor"`
	MCP            MCPConfig     `yaml:"mcp"`

	// ResolvedProvider 与 APIKey 一起由 Resolve 填充，可能因兜底而不同于 Provider。
	ResolvedProvider string `yaml:"-"`
	FellBackToRouter bool   `yaml:"-"`
}

// Load 读取 YAML 配置，文件不存在时使用默认值；随后加载 .env 并套用环境变量覆盖。
func Load(path string) (*Config, error) {
	cfg := defaults()

	if raw, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("解析配置文件 %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取配置文件 %s: %w", path, err)
	}

	LoadDotEnv()
	cfg.applyEnv()
	cfg.normalize()
	return cfg, nil
}

func defaults() *Config {
	return &Config{
		Provider:       "kimi",
		Temperature:    0.7,
		MaxTokens:      4096,
		TimeoutSeconds: 180,
		MaxIterations:  20,
		SystemPrompt:   DefaultSystemPrompt,
		Server:         ServerConfig{Host: "0.0.0.0", Port: 8000},
		Hints:          HintsConfig{TrajectoryFile: "event_agent_trajectory.json"},
		Monitor: MonitorConfig{
			IntervalSeconds: 10, UserTimeoutSeconds: 60, ProcessTimeoutSeconds: 30,
		},
	}
}

// applyEnv 套用环境变量覆盖，字段与 Python 版 env.example 一致。
func (c *Config) applyEnv() {
	if value := os.Getenv("LLM_PROVIDER"); value != "" {
		c.Provider = value
	}
	if value := os.Getenv("LLM_MODEL"); value != "" {
		c.Model = value
	}
	if value := os.Getenv("AGENT_HOST"); value != "" {
		c.Server.Host = value
	}
	c.Server.Port = envInt("AGENT_PORT", c.Server.Port)
	if value := os.Getenv("ENABLE_MCP_TOOLS"); value != "" {
		c.MCP.Enabled = !isFalsey(value)
	}
}

func (c *Config) normalize() {
	c.Provider = NormalizeProvider(c.Provider)
	if c.Temperature <= 0 {
		c.Temperature = 0.7
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = 4096
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = 180
	}
	if c.MaxIterations <= 0 {
		c.MaxIterations = 20
	}
	if c.Server.Port <= 0 {
		c.Server.Port = 8000
	}
	if c.SystemPrompt == "" {
		c.SystemPrompt = DefaultSystemPrompt
	}
	if c.Monitor.IntervalSeconds <= 0 {
		c.Monitor.IntervalSeconds = 10
	}
	if c.Monitor.UserTimeoutSeconds <= 0 {
		c.Monitor.UserTimeoutSeconds = 60
	}
	if c.Monitor.ProcessTimeoutSeconds <= 0 {
		c.Monitor.ProcessTimeoutSeconds = 30
	}
}

// HintConfig 把 YAML 开关转成领域配置，未配置的开关沿用默认开启。
func (c *Config) HintConfig() entity.SystemHintConfig {
	hints := entity.DefaultSystemHintConfig()
	applyBool(&hints.EnableTimestamps, c.Hints.EnableTimestamps)
	applyBool(&hints.EnableToolCounter, c.Hints.EnableToolCounter)
	applyBool(&hints.EnableTodoList, c.Hints.EnableTodoList)
	applyBool(&hints.EnableDetailedErrors, c.Hints.EnableDetailedErrors)
	applyBool(&hints.EnableSystemState, c.Hints.EnableSystemState)
	applyBool(&hints.SimulateTimeDelay, c.Hints.SimulateTimeDelay)
	applyBool(&hints.SaveTrajectory, c.Hints.SaveTrajectory)
	if c.Hints.TrajectoryFile != "" {
		hints.TrajectoryFile = c.Hints.TrajectoryFile
	}
	return hints
}

// ChatOptions 把配置转成单次请求的采样参数，推理模型的温度自动改写为 1。
func (c *Config) ChatOptions(model string) entity.ChatOptions {
	return entity.ChatOptions{
		Temperature: entity.SafeTemperature(model, c.Temperature),
		MaxTokens:   c.MaxTokens,
		ToolChoice:  "auto",
	}
}

func applyBool(target *bool, override *bool) {
	if override != nil {
		*target = *override
	}
}

func envInt(name string, fallback int) int {
	raw, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		// 与 Python 的 _env_int 一样容错：格式错误时回落默认值，不让服务起不来。
		return fallback
	}
	return parsed
}

func isFalsey(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "false", "0", "no":
		return true
	default:
		return false
	}
}

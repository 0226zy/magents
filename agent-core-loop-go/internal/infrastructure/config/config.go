// Package config 负责从 YAML 文件加载模型 SDK 配置。
package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 对应 config.yaml，模型 SDK 相关配置全部来自文件。
type Config struct {
	APIKey         string `yaml:"api_key"`
	BaseURL        string `yaml:"base_url"`
	Model          string `yaml:"model"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	MaxIterations  int    `yaml:"max_iterations"`
	LogDir         string `yaml:"log_dir"`
	// EnableRawLog 用指针区分「未配置」与「显式 false」，未配置时默认开启。
	EnableRawLog *bool  `yaml:"enable_raw_log"`
	SystemPrompt string `yaml:"system_prompt"`
}

// Load 从 YAML 文件读取配置；相对路径基于当前工作目录。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件 %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s: %w", path, err)
	}

	// 允许用环境变量覆盖文件里的 api_key，避免把密钥写进配置文件
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("MOONSHOT_API_KEY")
	}
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("KIMI_API_KEY")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("api_key 为空：请在 config.yaml 中填写，或设置环境变量 MOONSHOT_API_KEY / KIMI_API_KEY")
	}
	if cfg.BaseURL == "" {
		return nil, errors.New("base_url 不能为空")
	}
	if cfg.Model == "" {
		return nil, errors.New("model 不能为空")
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 120
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 8
	}
	if cfg.LogDir == "" {
		cfg.LogDir = "logs"
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = "You are a helpful assistant. Use tools to get real-time information when needed."
	}
	return &cfg, nil
}

// RawLogEnabled 报告是否记录模型原始响应（未配置时默认开启）。
func (c *Config) RawLogEnabled() bool {
	return c.EnableRawLog == nil || *c.EnableRawLog
}

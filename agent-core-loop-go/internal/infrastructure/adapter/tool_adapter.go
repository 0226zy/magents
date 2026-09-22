package adapter

import (
	"encoding/json"
	"fmt"

	"github.com/0226zy/magents/agent-core-loop-go/internal/domain/entity"
)

// ToolRegistry 是 domain/port.ToolPort 的默认实现：按名字分发到具体工具函数。
// get_current_time 与 get_weather 均为桩实现——不接任何真实 API，调用一律返回固定默认值。
type ToolRegistry struct {
	definitions []entity.ToolDefinition
	executors   map[string]func(argumentsJSON string) (string, error)
}

// NewToolRegistry 构造包含示例工具的注册表。
func NewToolRegistry() *ToolRegistry {
	r := &ToolRegistry{executors: map[string]func(string) (string, error){}}
	r.register(currentTimeDefinition(), executeCurrentTime)
	r.register(weatherDefinition(), executeWeather)
	return r
}

func (r *ToolRegistry) register(def entity.ToolDefinition, fn func(string) (string, error)) {
	r.definitions = append(r.definitions, def)
	r.executors[def.Function.Name] = fn
}

// Definitions 返回提供给模型的工具声明。
func (r *ToolRegistry) Definitions() []entity.ToolDefinition { return r.definitions }

// Execute 按名字执行工具，解析模型生成的 JSON 字符串参数。
func (r *ToolRegistry) Execute(name string, argumentsJSON string) (string, error) {
	fn, ok := r.executors[name]
	if !ok {
		return "", fmt.Errorf("未注册的工具: %s", name)
	}
	return fn(argumentsJSON)
}

// ── get_current_time ────────────────────────────────────────────────

func currentTimeDefinition() entity.ToolDefinition {
	return entity.ToolDefinition{
		Type: "function",
		Function: entity.FunctionSpec{
			Name:        "get_current_time",
			Description: "Get the current date and time in a specific timezone",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"timezone": map[string]any{
						"type":        "string",
						"description": "Timezone name, e.g. America/Vancouver",
					},
				},
				"required": []string{"timezone"},
			},
		},
	}
}

type currentTimeArgs struct {
	Timezone string `json:"timezone"`
}

// executeCurrentTime 是桩实现：不接真实时钟/外部 API，返回固定默认值。
func executeCurrentTime(argumentsJSON string) (string, error) {
	var args currentTimeArgs
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return "", fmt.Errorf("解析参数失败: %w", err)
	}
	result, _ := json.Marshal(map[string]any{
		"datetime":    "2025-09-13T05:18:47",
		"day_of_week": "Saturday",
		"timezone":    args.Timezone,
	})
	return string(result), nil
}

// ── get_weather ─────────────────────────────────────────────────────

func weatherDefinition() entity.ToolDefinition {
	return entity.ToolDefinition{
		Type: "function",
		Function: entity.FunctionSpec{
			Name:        "get_weather",
			Description: "Get the current weather for a specific city",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{
						"type":        "string",
						"description": "City name",
					},
					"unit": map[string]any{
						"type":        "string",
						"description": "Temperature unit",
						"enum":        []string{"celsius", "fahrenheit"},
					},
				},
				"required": []string{"city"},
			},
		},
	}
}

type weatherArgs struct {
	City string `json:"city"`
	Unit string `json:"unit"`
}

// executeWeather 是桩实现：接入真实天气 API 前返回固定默认值。
func executeWeather(argumentsJSON string) (string, error) {
	var args weatherArgs
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return "", fmt.Errorf("解析参数失败: %w", err)
	}
	unit := args.Unit
	if unit == "" {
		unit = "celsius"
	}
	result, _ := json.Marshal(map[string]any{
		"temperature": 13.2,
		"unit":        unit,
		"conditions":  "clear",
		"humidity":    93,
		"city":        args.City,
	})
	return string(result), nil
}

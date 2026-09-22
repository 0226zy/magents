// Package session 把端口装配好的客户端与 ReAct 循环组合成「问一个问题」的用例。
//
// 两个命令行入口共用它，因此运行载荷的形状（provider / model / base_url /
// question / answer / trace / api_turns）只定义一次。
//
// 本包只依赖 domain（entity/port）与 application/react：
// 具体客户端的构造（HTTP、凭据）属于组合根，见 shared/deps。
package session

import (
	"context"
	"io"
	"time"

	"github.com/0226zy/magents/web-search-agent-go/internal/application/react"
	"github.com/0226zy/magents/web-search-agent-go/internal/domain/entity"
	"github.com/0226zy/magents/web-search-agent-go/internal/domain/port"
)

// Payload 是一次独立提问的完整记录，字段与 Python 版 run_once 的返回一致。
type Payload struct {
	Provider string           `json:"provider"`
	Model    string           `json:"model"`
	BaseURL  string           `json:"base_url"`
	Question string           `json:"question"`
	Answer   string           `json:"answer"`
	Trace    []react.Step     `json:"trace"`
	APITurns []entity.APITurn `json:"api_turns"`
}

// Options 控制会话行为。
type Options struct {
	Timeout          time.Duration
	Verbose          bool
	Output           io.Writer
	MaxParallelTools int
	ReuseDeclaration bool
}

// Session 持有一个问题所需的全部客户端与状态。
//
// 它不是并发安全的：每次 Ask 都会重置 Agent 的历史、轨迹与凭据。
// 批量并发场景请为每个问题开一个 Session。
type Session struct {
	backend  entity.Backend
	recorder *entity.Recorder
	agent    *react.Agent
}

// Open 用已构造好的端口装配一个会话。组合根（shared/deps）负责建客户端。
func Open(backend entity.Backend, chat port.ChatCaller, formula port.FormulaRunner, recorder *entity.Recorder, options Options) *Session {
	agent := react.New(chat, formula, react.Options{
		ReuseDeclaration: options.ReuseDeclaration,
		MaxParallelTools: options.MaxParallelTools,
		Timeout:          options.Timeout,
		Verbose:          options.Verbose,
		Output:           options.Output,
		Recorder:         recorder,
		NoSearch:         backend.UsingOpenRouter(),
	})
	return &Session{backend: backend, recorder: recorder, agent: agent}
}

// Ask 回答一个问题，并连同完整的 provider 凭据一起返回。
func (s *Session) Ask(ctx context.Context, question string, maxIterations int) (Payload, react.Result) {
	result := s.agent.Run(ctx, question, maxIterations)
	return Payload{
		Provider: s.backend.Provider,
		Model:    s.backend.Model,
		BaseURL:  s.backend.BaseURL,
		Question: question,
		Answer:   result.Answer,
		Trace:    result.Trace,
		APITurns: s.recorder.Turns(),
	}, result
}

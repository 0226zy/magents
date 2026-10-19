package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/entity"
)

// ClientOptions 是事件客户端的命令行参数。
type ClientOptions struct {
	Server    string
	Mode      string
	Message   string
	EventType string
}

// ParseClientOptions 解析事件客户端参数。
func ParseClientOptions(args []string) (ClientOptions, error) {
	var opts ClientOptions
	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	fs.StringVar(&opts.Server, "server", "http://localhost:8000", "服务器地址")
	fs.StringVar(&opts.Mode, "mode", "test", "模式：test（预置场景）或 interactive（交互式）")
	fs.StringVar(&opts.Message, "message", "", "发送单条事件的内容；提供时忽略 -mode，发完即退出")
	fs.StringVar(&opts.EventType, "event-type", string(entity.EventWebMessage),
		"-message 使用的事件类型，可选："+eventTypeList())
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.Mode != "test" && opts.Mode != "interactive" {
		return opts, fmt.Errorf("未知模式 %q，只支持 test / interactive", opts.Mode)
	}
	if _, err := entity.ParseEventType(opts.EventType); err != nil {
		return opts, err
	}
	return opts, nil
}

// RunClient 执行事件客户端。
func RunClient(ctx context.Context, opts ClientOptions, out io.Writer) error {
	client := NewClient(opts.Server)

	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := client.Get(healthCtx, "/health"); err != nil {
		fmt.Fprintf(out, "无法连接服务器 %s: %v\n", opts.Server, err)
		fmt.Fprintln(out, "提示：先启动服务端 go run ./cmd/server")
		return err
	}
	fmt.Fprintf(out, "已连接服务器 %s\n", opts.Server)

	switch {
	case opts.Message != "":
		return sendEvent(ctx, client, out, opts.EventType, opts.Message, nil)
	case opts.Mode == "interactive":
		return interactiveMode(ctx, client, out, os.Stdin)
	default:
		return testScenarios(ctx, client, out)
	}
}

func sendEvent(ctx context.Context, client *Client, out io.Writer, eventType, content string,
	metadata map[string]any) error {

	fmt.Fprintf(out, "\n发送事件 [%s]: %s\n", eventType, content)
	result, err := client.Post(ctx, "/event", map[string]any{
		"event_type": eventType,
		"content":    content,
		"metadata":   metadata,
	})
	if err != nil {
		fmt.Fprintf(out, "失败: %v\n", err)
		return err
	}
	printResult(out, result)
	return nil
}

func printResult(out io.Writer, result map[string]any) {
	if answer, ok := result["final_answer"].(string); ok && answer != "" {
		fmt.Fprintf(out, "回答: %s\n", answer)
	} else {
		fmt.Fprintln(out, "回答: (未给出最终回答)")
	}
	fmt.Fprintf(out, "轮次: %v，本次工具调用: %v，累计: %v，TODO: %v\n",
		result["iterations"], result["tool_calls_count"],
		result["tool_calls_total"], result["todo_items"])
}

// scenario 是一个预置测试场景。
type scenario struct {
	title     string
	eventType entity.EventType
	content   string
	metadata  map[string]any
}

// testScenarios 依次发送多种事件，覆盖全部事件来源。与 Python 版场景一一对应。
func testScenarios(ctx context.Context, client *Client, out io.Writer) error {
	scenarios := []scenario{
		{"网页消息", entity.EventWebMessage,
			"Hello! Can you create a simple Python script that prints 'Hello, World!'?",
			map[string]any{"user_id": "user123", "session_id": "session456"}},
		{"即时通讯消息", entity.EventIMMessage,
			"Can you list the files in the current directory?",
			map[string]any{"sender": "Alice", "platform": "Slack"}},
		{"邮件回复", entity.EventEmailReply,
			"Thanks for the report! Can you also check the disk usage?",
			map[string]any{"from": "bob@example.com", "subject": "Re: System Report",
				"thread_id": "thread789"}},
		{"GitHub PR 更新", entity.EventGitHubPRUpdate,
			"Review comment: Please add unit tests for the new feature.",
			map[string]any{"pr_number": "42", "action": "review_requested",
				"reviewer": "code-reviewer", "repository": "ai-agent-project"}},
		{"定时触发", entity.EventTimerTrigger,
			"Daily backup reminder - please check if backups are running correctly.",
			map[string]any{"timer_id": "daily_backup_check", "schedule": "daily at 09:00"}},
		{"系统告警", entity.EventSystemAlert,
			"Memory usage has exceeded 80%. Please investigate.",
			map[string]any{"alert_type": "resource_usage", "severity": "warning",
				"memory_usage": "82%"}},
	}

	fmt.Fprintln(out, "\n===== 事件驱动 Agent 测试场景 =====")
	for i, item := range scenarios {
		fmt.Fprintf(out, "\n场景 %d：%s\n%s\n", i+1, item.title, strings.Repeat("-", 60))
		if err := sendEvent(ctx, client, out, string(item.eventType), item.content, item.metadata); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "\n场景 %d：注册后台进程\n%s\n", len(scenarios)+1, strings.Repeat("-", 60))
	registered, err := client.Post(ctx, "/process/register", map[string]any{
		"process_id": "proc_ml_training", "process_name": "ML Model Training",
	})
	if err != nil {
		return err
	}
	printJSON(out, registered)

	fmt.Fprintf(out, "\n场景 %d：启动超时监控\n%s\n", len(scenarios)+2, strings.Repeat("-", 60))
	monitoring, err := client.Post(ctx, "/monitoring/start", map[string]any{})
	if err != nil {
		return err
	}
	printJSON(out, monitoring)
	fmt.Fprintln(out, "监控已启动，用户空闲与进程超时会自动产生提醒事件。")

	fmt.Fprintf(out, "\nAgent 当前状态\n%s\n", strings.Repeat("-", 60))
	status, err := client.Get(ctx, "/agent/status")
	if err != nil {
		return err
	}
	printJSON(out, status)

	fmt.Fprintln(out, "\n===== 测试场景执行完毕 =====")
	return nil
}

// interactiveMode 从标准输入读取事件。
func interactiveMode(ctx context.Context, client *Client, out io.Writer, in io.Reader) error {
	fmt.Fprintln(out, "\n交互模式。命令：status / reset / quit；其余输入作为 web_message 发送。")
	fmt.Fprintf(out, "指定类型用 <事件类型>:<内容>，可选类型：%s\n", eventTypeList())

	scanner := bufio.NewScanner(in)
	for {
		fmt.Fprint(out, "\n> ")
		if !scanner.Scan() {
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
			continue
		case line == "quit" || line == "exit":
			return nil
		case line == "status":
			status, err := client.Get(ctx, "/agent/status")
			if err != nil {
				fmt.Fprintf(out, "失败: %v\n", err)
				continue
			}
			printJSON(out, status)
			continue
		case line == "reset":
			if _, err := client.Post(ctx, "/agent/reset", map[string]any{}); err != nil {
				fmt.Fprintf(out, "失败: %v\n", err)
				continue
			}
			fmt.Fprintln(out, "Agent 状态已重置")
			continue
		}

		eventType, content := splitTypedInput(line)
		_ = sendEvent(ctx, client, out, eventType, content, nil)
	}
}

// splitTypedInput 解析 "<事件类型>:<内容>"；前缀不是合法事件类型时整行当作网页消息。
func splitTypedInput(line string) (string, string) {
	prefix, rest, found := strings.Cut(line, ":")
	if !found {
		return string(entity.EventWebMessage), line
	}
	if _, err := entity.ParseEventType(strings.TrimSpace(prefix)); err != nil {
		return string(entity.EventWebMessage), line
	}
	return strings.TrimSpace(prefix), strings.TrimSpace(rest)
}

// eventTypeList 渲染全部事件类型，用于命令行帮助文本。
func eventTypeList() string {
	types := entity.AllEventTypes()
	names := make([]string, len(types))
	for i, eventType := range types {
		names[i] = string(eventType)
	}
	return strings.Join(names, " / ")
}

func printJSON(out io.Writer, payload any) {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(payload)
}

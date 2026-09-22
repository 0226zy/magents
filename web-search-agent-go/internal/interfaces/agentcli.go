// Package interfaces 是对外协议适配层（CLI/批处理入口）：
// 解析命令行参数、调 application 用例、把结果写给用户，不含业务逻辑。
package interfaces

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/0226zy/magents/web-search-agent-go/internal/application/react"
	"github.com/0226zy/magents/web-search-agent-go/internal/application/session"
	"github.com/0226zy/magents/web-search-agent-go/internal/infrastructure/config"
	"github.com/0226zy/magents/web-search-agent-go/internal/infrastructure/evidence"
	"github.com/0226zy/magents/web-search-agent-go/internal/shared/deps"
)

const separator = "============================================================"

// providerFlag 限定后端取值。标准库 flag 不支持枚举校验，所以自定义 flag.Value。
type providerFlag string

func (p *providerFlag) String() string { return string(*p) }

func (p *providerFlag) Set(value string) error {
	switch value {
	case "kimi", "offline-demo":
		*p = providerFlag(value)
		return nil
	}
	return fmt.Errorf("只支持 kimi 或 offline-demo，收到 %q", value)
}

type agentCLIOptions struct {
	provider      providerFlag
	model         string
	maxSteps      int
	baseURL       string
	apiKey        string
	output        string
	quiet         bool
	allowNoSearch bool
}

// RunAgentCLI 是 agentcli 命令的转发入口。
//
// 演示第一章的 ReAct 循环（Reasoning + Acting）：模型先思考，再调用 web_search
// 行动，观察搜索结果后继续思考，直到综合出最终答案。运行时会逐步打印 ReAct 轨迹。
//
// 用法:
//
//	agentcli                                   # 交互模式
//	agentcli "2024 诺贝尔物理学奖得主是谁？"      # 单次问答，打印 ReAct 轨迹
//	agentcli -provider offline-demo             # 离线演示 ReAct 循环（无需 API Key）
//	agentcli "比特币现价" -max-steps 3 -output result.json
func RunAgentCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	opts := agentCLIOptions{provider: "kimi"}
	flags := flag.NewFlagSet("agentcli", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Var(&opts.provider, "provider",
		"搜索后端：kimi=调用 Kimi Formula web_search（需 API Key）；offline-demo=离线回放示例轨迹")
	flags.StringVar(&opts.model, "model", config.DefaultModel, "使用的模型名称")
	flags.IntVar(&opts.maxSteps, "max-steps", config.DefaultMaxIterations, "最大 ReAct 迭代次数")
	flags.StringVar(&opts.baseURL, "base-url", "", "API 基础 URL（默认取 KIMI_BASE_URL 或官方端点）")
	flags.StringVar(&opts.apiKey, "api-key", "", "Kimi API Key（默认从 MOONSHOT_API_KEY / KIMI_API_KEY 读取）")
	flags.StringVar(&opts.output, "output", "", "保存问答与 ReAct 轨迹：单次模式写 JSON，交互模式按行追加 JSONL")
	flags.StringVar(&opts.output, "o", "", "-output 的简写")
	flags.BoolVar(&opts.quiet, "quiet", false, "不实时打印 ReAct 轨迹")
	flags.BoolVar(&opts.allowNoSearch, "allow-no-search", false,
		"允许在没有 Moonshot 凭据时退回 OpenRouter，由模型凭自身知识作答（无实时联网搜索）")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Kimi Web Search Agent —— 演示 ReAct 循环（思考→行动→观察）的搜索 Agent。")
		fmt.Fprintln(stderr, "\n用法: agentcli [选项] [问题...]\n\n选项:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	question := strings.TrimSpace(strings.Join(flags.Args(), " "))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if opts.provider == "offline-demo" {
		return runOfflineDemo(opts, question, stdout)
	}

	backend, err := config.Resolve(opts.apiKey, opts.baseURL, opts.model, opts.allowNoSearch)
	if err != nil {
		return err
	}
	if backend.UsingOpenRouter() {
		fmt.Fprintf(stderr,
			"\n⚠️  警告：未找到 Moonshot 凭据，本次运行改走 OpenRouter（模型 %s）。\n"+
				"    Moonshot Formula web_search 在 OpenRouter 上不可用，模型将仅凭自身知识作答，\n"+
				"    不做任何实时联网搜索，答案可能过时。\n\n", backend.Model)
	}

	agentSession := deps.NewSession(backend, session.Options{
		Timeout: config.SearchTimeout(),
		Verbose: !opts.quiet,
		Output:  stdout,
	})

	if question != "" {
		return runSingleQuestion(ctx, agentSession, opts, question, stdout)
	}
	return runInteractive(ctx, agentSession, opts, stdin, stdout, stderr)
}

func runOfflineDemo(opts agentCLIOptions, question string, stdout io.Writer) error {
	fmt.Fprintf(stdout, "\n%s\n🧪 离线演示模式（示例轨迹，非真实搜索结果）\n%s\n", separator, separator)
	fmt.Fprintf(stdout, "问题: %s\n", firstNonEmpty(question, react.DefaultOfflineQuestion))
	fmt.Fprintf(stdout, "%s\n🔍 ReAct 轨迹（想 → 做 → 看）:\n\n", strings.Repeat("-", 60))

	result := react.RunOfflineDemo(question, !opts.quiet, stdout)

	fmt.Fprintf(stdout, "\n📝 答案:\n%s\n%s\n%s\n\n", strings.Repeat("-", 60), result.Answer, separator)
	if opts.output == "" {
		return nil
	}
	if _, err := evidence.Write(opts.output, result); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "💾 结果已保存到: %s\n", opts.output)
	return nil
}

func runSingleQuestion(ctx context.Context, agentSession *session.Session, opts agentCLIOptions, question string, stdout io.Writer) error {
	fmt.Fprintf(stdout, "\n%s\n🤖 Kimi Web Search Agent\n%s\n", separator, separator)
	fmt.Fprintf(stdout, "问题: %s\n%s\n🔍 ReAct 轨迹（想 → 做 → 看）:\n\n", question, strings.Repeat("-", 60))

	payload, _ := agentSession.Ask(ctx, question, opts.maxSteps)

	fmt.Fprintf(stdout, "\n📝 答案:\n%s\n%s\n%s\n\n",
		strings.Repeat("-", 60), payload.Answer, separator)
	if opts.output == "" {
		return nil
	}
	if _, err := evidence.Write(opts.output, payload); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "💾 结果已保存到: %s\n", opts.output)
	return nil
}

// runInteractive 是交互模式。
//
// 每个问题都是独立的一次运行：Ask 会重置对话历史，问题之间没有上下文。
// 因此这里既没有「多轮对话」，也没有可清空的历史。
func runInteractive(ctx context.Context, agentSession *session.Session, opts agentCLIOptions, stdin io.Reader, stdout, stderr io.Writer) error {
	fmt.Fprintf(stdout, "\n%s\n🤖 Kimi Web Search Agent - 交互模式\n%s\n", separator, separator)
	fmt.Fprintln(stdout, "输入您的问题，Agent 将自动搜索并回答")
	fmt.Fprintln(stdout, "每个问题相互独立：Agent 不保留跨问题的上下文")
	fmt.Fprintln(stdout, "输入 'quit' 或 'exit' 退出")
	if opts.output != "" {
		fmt.Fprintf(stdout, "每次问答都会追加写入: %s\n", opts.output)
	}
	fmt.Fprintf(stdout, "%s\n\n", separator)

	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		fmt.Fprint(stdout, "您的问题: ")
		if !scanner.Scan() {
			fmt.Fprintln(stdout, "\n\n👋 再见！")
			return scanner.Err()
		}
		question := strings.TrimSpace(scanner.Text())

		switch strings.ToLower(question) {
		case "quit", "exit", "q":
			fmt.Fprintln(stdout, "\n👋 再见！")
			return nil
		case "":
			fmt.Fprint(stdout, "❌ 请输入一个问题\n\n")
			continue
		}

		fmt.Fprint(stdout, "\n🔍 Agent 正在搜索和思考（ReAct 轨迹如下）...\n\n")
		payload, _ := agentSession.Ask(ctx, question, opts.maxSteps)

		fmt.Fprintf(stdout, "\n%s\n📝 Agent 回答:\n%s\n%s\n%s\n\n",
			separator, strings.Repeat("-", 60), payload.Answer, separator)

		if opts.output == "" {
			continue
		}
		if err := evidence.AppendJSONL(opts.output, payload); err != nil {
			fmt.Fprintf(stderr, "⚠️  保存失败: %v\n", err)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

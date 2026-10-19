package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/shared/deps"
)

// EventLoopOptions 是事件循环演示的命令行参数。
type EventLoopOptions struct {
	Trigger       string
	Mock          bool
	Duration      time.Duration
	Delay         time.Duration
	Interval      time.Duration
	WatchDir      string
	WatchPoll     time.Duration
	Provider      string
	Model         string
	MaxIterations int
	ConfigPath    string
}

// ParseEventLoopOptions 解析事件循环演示参数。
func ParseEventLoopOptions(args []string) (EventLoopOptions, error) {
	var opts EventLoopOptions
	var duration, delay, interval, watchPoll float64

	fs := flag.NewFlagSet("eventloop", flag.ContinueOnError)
	fs.StringVar(&opts.Trigger, "trigger", "all", "触发器：timer / recurring / file / all")
	fs.BoolVar(&opts.Mock, "mock", false, "离线模式：不调用大模型，用模拟动作演示闭环")
	fs.Float64Var(&duration, "duration", 12, "事件循环总运行时长（秒）")
	fs.Float64Var(&delay, "delay", 3, "一次性定时器的延迟（秒）")
	fs.Float64Var(&interval, "interval", 4, "循环定时器的间隔（秒）")
	fs.StringVar(&opts.WatchDir, "watch-dir", "watched_dir", "文件监听目录，不存在会自动创建")
	fs.Float64Var(&watchPoll, "watch-poll", 1, "文件监听的轮询间隔（秒）")
	fs.StringVar(&opts.Provider, "provider", "", "真实模式的提供商："+
		strings.Join(deps.SupportedProviders(), " / "))
	fs.StringVar(&opts.Model, "model", "", "真实模式的模型名覆盖")
	fs.IntVar(&opts.MaxIterations, "max-iterations", 10, "真实模式下单个事件的最大工具轮数")
	fs.StringVar(&opts.ConfigPath, "config", "config.yaml", "配置文件路径")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}

	switch opts.Trigger {
	case "timer", "recurring", "file", "all":
	default:
		return opts, fmt.Errorf("未知触发器 %q，只支持 timer / recurring / file / all", opts.Trigger)
	}

	opts.Duration = seconds(duration)
	opts.Delay = seconds(delay)
	opts.Interval = seconds(interval)
	opts.WatchPoll = seconds(watchPoll)
	return opts, nil
}

// RunEventLoop 执行事件循环演示。
func RunEventLoop(ctx context.Context, opts EventLoopOptions, out io.Writer) error {
	log := deps.NewLogger(out)

	mode := "真实 Agent"
	if opts.Mock {
		mode = "离线模拟"
	}
	fmt.Fprintf(out, "\n事件驱动 Agent 演示 | 触发器: %s | 模式: %s | 时长: %.0fs\n%s\n",
		opts.Trigger, mode, opts.Duration.Seconds(), strings.Repeat("=", 60))

	all := opts.Trigger == "all"
	spec := deps.EventLoopSpec{
		Mock:          opts.Mock,
		Timer:         all || opts.Trigger == "timer",
		Recurring:     all || opts.Trigger == "recurring",
		FileWatch:     all || opts.Trigger == "file",
		Delay:         opts.Delay,
		Interval:      opts.Interval,
		WatchDir:      opts.WatchDir,
		WatchPoll:     opts.WatchPoll,
		ConfigPath:    opts.ConfigPath,
		Provider:      opts.Provider,
		Model:         opts.Model,
		MaxIterations: opts.MaxIterations,
	}
	if spec.FileWatch {
		fmt.Fprintf(out, "提示：向 %s/ 写入文件即可触发 file_change 事件，"+
			"例如 echo hello > %s/note.txt\n", opts.WatchDir, opts.WatchDir)
	}

	loop, cleanup, err := deps.BuildEventLoop(ctx, spec, log)
	if err != nil {
		return err
	}
	defer cleanup()

	if loop.Triggers() == 0 {
		return errors.New("没有可运行的触发器")
	}

	runCtx, cancel := context.WithTimeout(ctx, opts.Duration)
	defer cancel()
	loop.Run(runCtx)

	fmt.Fprintf(out, "\n%s\n演示结束：共处理 %d 个事件。\n", strings.Repeat("=", 60), loop.Processed())
	return nil
}

func seconds(value float64) time.Duration {
	return time.Duration(value * float64(time.Second))
}

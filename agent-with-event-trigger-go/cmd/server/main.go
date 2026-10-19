// Command server 启动事件驱动 Agent 的 HTTP 服务。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/infrastructure/adapter"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/infrastructure/config"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/interfaces/httpapi"
	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/shared/deps"
)

// shutdownTimeout 给正在处理的事件留出收尾时间。
const shutdownTimeout = 30 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var configPath, host, provider, model string
	var port int

	flag.StringVar(&configPath, "config", "config.yaml", "配置文件路径")
	flag.StringVar(&host, "host", "", "监听地址，覆盖配置")
	flag.IntVar(&port, "port", 0, "监听端口，覆盖配置")
	flag.StringVar(&provider, "provider", "", "模型提供商，覆盖配置")
	flag.StringVar(&model, "model", "", "模型名，覆盖配置")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if host != "" {
		cfg.Server.Host = host
	}
	if port > 0 {
		cfg.Server.Port = port
	}
	if provider != "" {
		cfg.Provider = provider
	}
	if model != "" {
		cfg.Model = model
	}

	log := adapter.NewStdLogger()
	runtime, err := deps.Build(cfg, log)
	if err != nil {
		return err
	}
	defer runtime.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runtime.LoadMCPTools(ctx)
	if cfg.Monitor.Enabled {
		runtime.Monitor.Start(ctx)
	}

	address := net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port))
	server := &http.Server{
		Addr:              address,
		Handler:           httpapi.New(runtime.Agent, runtime.Monitor, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// 不设 WriteTimeout：一次事件处理可能跑好几分钟的模型调用，
		// 写超时会在服务端还在干活时把响应截断。
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("事件服务已启动：http://%s（provider=%s, model=%s）",
			address, cfg.ResolvedProvider, cfg.Model)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("收到退出信号，正在停止服务…")
	}

	runtime.Monitor.Stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

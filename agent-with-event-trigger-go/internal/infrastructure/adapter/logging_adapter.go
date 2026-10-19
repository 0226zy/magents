package adapter

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// StdLogger 是带级别与时间戳的行日志，实现 port.Logger。
type StdLogger struct {
	mu  sync.Mutex
	out io.Writer
}

// NewStdLogger 输出到 stderr，避免与 CLI 的结构化 stdout 输出混在一起。
func NewStdLogger() *StdLogger { return &StdLogger{out: os.Stderr} }

// NewLoggerTo 输出到指定 writer，供测试使用。
func NewLoggerTo(w io.Writer) *StdLogger { return &StdLogger{out: w} }

// Info 记录一条信息级日志。
func (l *StdLogger) Info(format string, args ...any) { l.write("INFO", format, args...) }

// Warn 记录一条警告级日志。
func (l *StdLogger) Warn(format string, args ...any) { l.write("WARN", format, args...) }

// Error 记录一条错误级日志。
func (l *StdLogger) Error(format string, args ...any) { l.write("ERROR", format, args...) }

func (l *StdLogger) write(level, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "%s - %s - %s\n",
		time.Now().Format("2006-01-02 15:04:05"), level, fmt.Sprintf(format, args...))
}

// NopLogger 丢弃全部日志，供不需要输出的测试使用。
type NopLogger struct{}

func (NopLogger) Info(string, ...any)  {}
func (NopLogger) Warn(string, ...any)  {}
func (NopLogger) Error(string, ...any) {}

package tools

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/0226zy/magents/dsh-go/internal/domain/entity"
	"github.com/0226zy/magents/dsh-go/internal/domain/port"
)

// errShellDied 表示命令尚未写入时 bash 会话已死，调用方可安全重启重试。
var errShellDied = errors.New("bash 会话已终止")

// errShellExited 表示命令写入后 bash 中途退出（如命令本身含 exit）：
// 命令可能已部分执行，退出码不可知。
var errShellExited = errors.New("bash 会话意外退出")

// BashTool 是持久化终端会话工具（指引 §2.3.1 #2）：
// 每个会话（session）持有自己的 bash 进程，cwd / 环境变量 / 后台作业跨调用保持。
type BashTool struct {
	timeout    time.Duration
	maxOutput  int
	mu         sync.Mutex
	sessions   map[string]*shell
	defaultCwd func(sessionID string) string
}

// NewBashTool 构造工具。defaultCwd 给出会话的工作区根（bash 启动目录）。
func NewBashTool(timeout time.Duration, maxOutput int, defaultCwd func(sessionID string) string) *BashTool {
	return &BashTool{timeout: timeout, maxOutput: maxOutput, sessions: map[string]*shell{}, defaultCwd: defaultCwd}
}

// CwdFor 返回会话 bash 的当前目录；会话尚未启动 bash 时返回工作区根。
func (b *BashTool) CwdFor(sessionID string) string {
	b.mu.Lock()
	sh, ok := b.sessions[sessionID]
	b.mu.Unlock()
	if ok && !sh.isDead() {
		return sh.cwd()
	}
	if b.defaultCwd != nil {
		return b.defaultCwd(sessionID)
	}
	return "."
}

// CloseSession 终止会话的 bash 进程（会话清理时调用）。
func (b *BashTool) CloseSession(sessionID string) {
	b.mu.Lock()
	sh, ok := b.sessions[sessionID]
	delete(b.sessions, sessionID)
	b.mu.Unlock()
	if ok {
		sh.kill()
	}
}

// Definition 实现 port.Tool。
func (b *BashTool) Definition() entity.ToolDefinition {
	return entity.ToolDefinition{
		Type: "function",
		Function: entity.FunctionSpec{
			Name: "bash",
			Description: `在持久化 bash 会话中执行 shell 命令。工作目录、环境变量、后台作业在多次调用间保持。
何时使用：构建、测试、git 操作、包管理、运行脚本、查看进程等一切需要 shell 的场景。
何时不要用：读文件用 read_file（有行号）、按内容搜索用 grep、按文件名定位用 glob——专用工具的结果更结构化。
参数：command 为完整 shell 命令，可含管道与重定向，例如 "go build ./... && go test ./..."。
失败形态：返回非零退出码与输出（IsError=true）；超过 timeout_seconds 被中断时返回结构化超时错误与已捕获的输出。
注意：交互式命令（等待 stdin 输入）会卡住直到超时，请用非交互形式（如 --yes、--non-interactive）。`,
			Parameters: schema(map[string]any{
				"command":         strProp("要执行的 shell 命令，例如 \"git status --short\" 或 \"npm test\""),
				"timeout_seconds": intProp("可选，本条命令的超时秒数，默认取服务端配置（通常 120 秒）"),
			}, "command"),
		},
	}
}

// ConcurrencySafe 实现 port.Tool：shell 有状态，不可并行。
func (b *BashTool) ConcurrencySafe() bool { return false }

type bashArgs struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// Execute 实现 port.Tool。
func (b *BashTool) Execute(ctx context.Context, argsJSON string, tctx *port.ToolContext) entity.ToolResult {
	var args bashArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return entity.ToolResult{IsError: true, Content: fmt.Sprintf("参数解析失败（应为 JSON 对象，含必填字段 command: string）：%v", err)}
	}
	if strings.TrimSpace(args.Command) == "" {
		return entity.ToolResult{IsError: true, Content: "参数 command 不能为空"}
	}
	timeout := b.timeout
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}

	restarted := false
	sh := b.shellFor(tctx.SessionID, tctx.Cwd())
	output, exitCode, timedOut, err := sh.run(ctx, args.Command, timeout)
	if errors.Is(err, errShellDied) || errors.Is(err, errShellExited) {
		// 会话已死：换新会话重试一次，并告知模型 shell 状态丢失。
		// errShellExited 时命令可能已部分执行，重试仍失败则按错误回注。
		restarted = true
		sh = b.shellFor(tctx.SessionID, tctx.Cwd())
		output, exitCode, timedOut, err = sh.run(ctx, args.Command, timeout)
	}
	if restarted {
		output = "[提示] 上一个 bash 会话已终止（可能是执行了 exit 或超时被杀），本命令已在新会话中执行，之前的 shell 状态（cwd/环境变量）已丢失。\n" + output
	}
	if timedOut {
		// 指引 §5.2：结构化超时错误，而非静默杀死——给 Agent 修正策略的机会。
		out := TruncateOutput(output, b.maxOutput, func() string { return tctx.Spill("bash", output) })
		return entity.ToolResult{IsError: true, Content: fmt.Sprintf(
			"执行超过 %s 被终止（已发送中断信号）。最后输出如下：\n%s\n请改用更小的任务分片、增加 timeout_seconds，或改为后台运行后再轮询。",
			timeout, out)}
	}
	if err != nil {
		return entity.ToolResult{IsError: true, Content: fmt.Sprintf("bash 执行失败: %v\n%s", err, output)}
	}
	out := TruncateOutput(output, b.maxOutput, func() string { return tctx.Spill("bash", output) })
	if exitCode != 0 {
		return entity.ToolResult{IsError: true, Content: fmt.Sprintf("退出码 %d\n%s", exitCode, out)}
	}
	if strings.TrimSpace(out) == "" {
		out = "(命令成功，无输出)"
	}
	return entity.ToolResult{Content: out}
}

// shellFor 取会话的活 shell；不存在或已死则在工作区根新建。
func (b *BashTool) shellFor(sessionID, cwd string) *shell {
	b.mu.Lock()
	defer b.mu.Unlock()
	if sh, ok := b.sessions[sessionID]; ok && !sh.isDead() {
		return sh
	}
	if cwd == "" || cwd == "." {
		cwd = b.defaultCwd(sessionID)
	}
	sh := newShell(cwd)
	b.sessions[sessionID] = sh
	return sh
}

// ── 持久 bash 进程 ────────────────────────────────────────────────

// shell 封装一个长驻 bash 进程：命令经 stdin 写入，
// 用随机哨兵行识别一次命令的结束，并顺带取回退出码与最新 cwd。
type shell struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan string
	done   chan struct{}
	token  string // 随机哨兵，避免与用户输出撞车
	curCwd string
	once   sync.Once
}

func newShell(cwd string) *shell {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	sh := &shell{
		lines:  make(chan string, 256),
		done:   make(chan struct{}),
		token:  "__DSH_DONE_" + hex.EncodeToString(buf) + "__",
		curCwd: cwd,
	}
	if err := sh.start(cwd); err != nil {
		sh.markDead()
	}
	return sh
}

func (sh *shell) start(cwd string) error {
	cmd := exec.Command("bash")
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = append(os.Environ(), "TERM=dumb")
	// 独立进程组：超时时向整个组发 SIGINT，连同子进程一起中断，但尽量保住 bash 本体。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		return err
	}
	sh.cmd = cmd
	sh.stdin = stdin
	go sh.readLoop(pr)
	// 进程退出后关掉读端：让 readLoop 退出、lines 关闭，
	// run 的 select 能立刻感知死亡（io.PipeWriter 不会被 exec 关闭，必须自己关）。
	go func() { _ = cmd.Wait(); _ = pr.Close(); sh.markDead() }()
	return nil
}

func (sh *shell) readLoop(pr *io.PipeReader) {
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		sh.lines <- sc.Text()
	}
	close(sh.lines)
	sh.markDead()
}

func (sh *shell) isDead() bool {
	select {
	case <-sh.done:
		return true
	default:
		return false
	}
}

func (sh *shell) markDead() { sh.once.Do(func() { close(sh.done) }) }

func (sh *shell) kill() {
	if sh.cmd != nil && sh.cmd.Process != nil {
		_ = syscall.Kill(-sh.cmd.Process.Pid, syscall.SIGKILL)
		_ = sh.cmd.Process.Kill()
	}
	sh.markDead()
}

func (sh *shell) cwd() string { return sh.curCwd }

// run 执行一条命令，返回输出、退出码、是否超时中断。
// 命令尚未写入时发现会话已死，返回 errShellDied（由调用方重启重试）。
func (sh *shell) run(ctx context.Context, command string, timeout time.Duration) (output string, exitCode int, timedOut bool, err error) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	if sh.isDead() || sh.stdin == nil {
		return "", -1, false, errShellDied
	}

	// 命令包装：用户命令单独一行（避免与其尾部注释/续行粘连），
	// 然后记录退出码、打印哨兵与最新 cwd。
	wrapped := fmt.Sprintf("%s\n__dsh_rc=$?; echo %q$__dsh_rc; pwd\n", command, sh.token)
	if _, err = io.WriteString(sh.stdin, wrapped); err != nil {
		sh.markDead()
		return "", -1, false, errShellDied
	}

	var out strings.Builder
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case line, ok := <-sh.lines:
			if !ok {
				return out.String(), -1, false, fmt.Errorf("bash 会话意外退出")
			}
			if strings.HasPrefix(line, sh.token) {
				// 哨兵行的下一行是 pwd
				select {
				case pwdLine := <-sh.lines:
					sh.curCwd = strings.TrimSpace(pwdLine)
				case <-time.After(2 * time.Second):
				}
				rc := 0
				fmt.Sscanf(strings.TrimPrefix(line, sh.token), "%d", &rc)
				return out.String(), rc, false, nil
			}
			out.WriteString(line)
			out.WriteByte('\n')
		case <-timer.C:
			if sh.cmd != nil && sh.cmd.Process != nil {
				_ = syscall.Kill(-sh.cmd.Process.Pid, syscall.SIGINT)
			}
			grace := time.NewTimer(3 * time.Second)
			for drained := false; !drained; {
				select {
				case line, ok := <-sh.lines:
					if !ok {
						drained = true
						break
					}
					if strings.HasPrefix(line, sh.token) {
						select {
						case pwdLine := <-sh.lines:
							sh.curCwd = strings.TrimSpace(pwdLine)
						case <-time.After(time.Second):
						}
						return out.String(), -1, true, nil
					}
					out.WriteString(line)
					out.WriteByte('\n')
				case <-grace.C:
					drained = true
				}
			}
			if sh.cmd != nil && sh.cmd.Process != nil {
				_ = syscall.Kill(-sh.cmd.Process.Pid, syscall.SIGKILL)
			}
			sh.markDead()
			return out.String(), -1, true, nil
		case <-sh.done:
			// bash 进程已死（如命令执行了 exit）：取回缓冲输出，立刻失败而非等超时
			for {
				select {
				case line, ok := <-sh.lines:
					if !ok {
						return out.String(), -1, false, errShellExited
					}
					if strings.HasPrefix(line, sh.token) {
						// 哨兵先于死亡到达：按正常完成处理
						rc := 0
						fmt.Sscanf(strings.TrimPrefix(line, sh.token), "%d", &rc)
						return out.String(), rc, false, nil
					}
					out.WriteString(line)
					out.WriteByte('\n')
				default:
					return out.String(), -1, false, errShellExited
				}
			}
		case <-ctx.Done():
			return out.String(), -1, false, ctx.Err()
		}
	}
}

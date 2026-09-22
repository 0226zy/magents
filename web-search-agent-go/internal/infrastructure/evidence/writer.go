// Package evidence 负责把运行凭据写成可复核的证据文件。
//
// 它只做三件事：按固定规则序列化、落盘前扫描凭证、算 SHA-256。
// 具体某个实验的证据文档长什么样由调用方定义，因此这里不依赖协议层与验收层。
package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Marshal 复刻 Python 的 json.dumps(value, ensure_ascii=False, indent=2) + "\n"。
//
// 必须关掉 HTML 转义：证据里全是带 & 的 URL，默认会被写成 \u0026，
// 既不可读，也让两个语言产出的字节不再可比。json.MarshalIndent 关不掉转义，
// 所以这里用 Encoder。Encode 自动补的换行恰好对应 Python 的 + "\n"。
func Marshal(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Write 序列化并落盘，落盘前先做凭证扫描。
//
// 扫描不通过时返回错误且不创建文件——这是「拒绝写入」的全部含义。
func Write(path string, value any) ([]byte, error) {
	payload, err := Marshal(value)
	if err != nil {
		return nil, err
	}
	if err := AssertCredentialFree(string(payload)); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return nil, err
	}
	return payload, nil
}

// WriteWithChecksum 落盘证据，并在同目录写一份 <name>.sha256。
// 校验和文件的格式与 sha256sum 一致，可直接 `shasum -c`。
func WriteWithChecksum(path string, value any) (string, error) {
	payload, err := Write(path, value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])

	name := filepath.Base(path)
	checksumPath := filepath.Join(filepath.Dir(path),
		strings.TrimSuffix(name, filepath.Ext(name))+".sha256")
	line := fmt.Sprintf("%s  %s\n", digest, name)
	if err := os.WriteFile(checksumPath, []byte(line), 0o644); err != nil {
		return "", err
	}
	return digest, nil
}

// AppendJSONL 追加一行 JSON，用于交互模式逐问留痕。
//
// Python 版的交互模式每问都覆盖同一个 --output 文件，最后只剩一条记录。
// 追加写让一次会话里的每个问题都留下来。
func AppendJSONL(path string, value any) error {
	line, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := AssertCredentialFree(string(line)); err != nil {
		return err
	}
	if directory := filepath.Dir(path); directory != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(line, '\n'))
	return err
}

// Timestamp 生成证据目录名用的时间戳，对应 Python 的 strftime("%Y%m%dT%H%M%SZ")。
func Timestamp(at time.Time) string {
	return at.UTC().Format("20060102T150405Z")
}

// Host 记录跑这次实验的机器。
type Host struct {
	Platform string `json:"platform"`
	Go       string `json:"go"`
	Machine  string `json:"machine"`
}

// CurrentHost 采集当前主机信息。
func CurrentHost() Host {
	return Host{
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Go:       runtime.Version(),
		Machine:  runtime.GOARCH,
	}
}

// Repository 记录代码版本。commit / branch 用指针，拿不到时写 null 而不是空串。
type Repository struct {
	Commit        *string `json:"commit"`
	Branch        *string `json:"branch"`
	WorktreeDirty bool    `json:"worktree_dirty"`
}

// CurrentRepository 采集 git 信息；不在 git 仓库里时各字段为 null / false。
func CurrentRepository(ctx context.Context) Repository {
	status := gitValue(ctx, "status", "--porcelain")
	return Repository{
		Commit:        gitValue(ctx, "rev-parse", "HEAD"),
		Branch:        gitValue(ctx, "branch", "--show-current"),
		WorktreeDirty: status != nil && *status != "",
	}
}

func gitValue(ctx context.Context, args ...string) *string {
	output, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return nil
	}
	value := strings.TrimSpace(string(output))
	return &value
}

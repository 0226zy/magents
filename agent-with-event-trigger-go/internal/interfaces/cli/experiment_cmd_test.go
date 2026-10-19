package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 没有凭据时实验要停在 blocked 并留下完整证据目录。
// 什么都不写才是最糟的结果：事后无法证明实验到底跑没跑。
func TestRunExperimentWritesEvidenceWhenBlocked(t *testing.T) {
	t.Setenv("UNIPILE_DSN", "")
	t.Setenv("UNIPILE_ACCESS_TOKEN", "")

	root := t.TempDir()
	opts, err := ParseExperimentOptions([]string{
		"-campaign-id", "unit-run", "-validation-root", root,
		"-protocol", filepath.Join(root, "absent.json"),
	})
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	dir, err := RunExperiment(context.Background(), opts, &out)
	if err != nil {
		t.Fatalf("被阻塞的运行不应返回错误: %v", err)
	}

	summary := readJSON(t, filepath.Join(dir, "summary.json"))
	if summary["status"] != "blocked" {
		t.Errorf("终态 = %v，期望 blocked", summary["status"])
	}
	if summary["official_complete"] != false {
		t.Errorf("official_complete = %v，期望 false", summary["official_complete"])
	}
	if summary["blocker_or_error"] == nil {
		t.Error("被阻塞的运行应记录原因")
	}

	manifest := readJSON(t, filepath.Join(dir, "manifest.json"))
	files, _ := manifest["files"].([]any)
	if len(files) == 0 {
		t.Error("清单应列出已产出的证据文件")
	}

	latest := readJSON(t, filepath.Join(root, "latest.json"))
	if latest["campaign_id"] != "unit-run" {
		t.Errorf("latest.json = %v", latest)
	}
	if hash, _ := latest["manifest_sha256"].(string); len(hash) != 64 {
		t.Errorf("latest.json 应记录清单哈希，实际 = %v", latest["manifest_sha256"])
	}
}

func TestParseExperimentOptionsGeneratesCampaignID(t *testing.T) {
	opts, err := ParseExperimentOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.CampaignID) != len("20060102T150405Z") {
		t.Errorf("自动生成的 campaign-id = %q", opts.CampaignID)
	}
	if opts.PollTimeout.Seconds() != 180 || opts.PollInterval.Seconds() != 5 {
		t.Errorf("轮询默认值 = %s / %s", opts.PollTimeout, opts.PollInterval)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("%s 不是合法 JSON: %v", path, err)
	}
	return payload
}

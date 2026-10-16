package loader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkdownLoaderLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	content := "# 标题一\n\n正文内容。\n\n## 小节\n\n更多内容。\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	l := MarkdownLoader{}
	doc, err := l.Load(path)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if doc.Title != "标题一" {
		t.Errorf("Title = %q, 期望 标题一", doc.Title)
	}
	if doc.Source != path {
		t.Errorf("Source = %q, 期望 %q", path, doc.Source)
	}
	if doc.Content != content {
		t.Error("Content 与原文不一致")
	}
}

func TestMarkdownLoaderNoTitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.md")
	if err := os.WriteFile(path, []byte("没有标题。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := (MarkdownLoader{}).Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "" {
		t.Errorf("无标题时期望空 Title，得到 %q", doc.Title)
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n\n内容A。\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte(" ignored "), 0o644) // 不支持的扩展名应被忽略
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "c.md"), []byte("# C\n\n内容C。\n"), 0o644)

	docs, err := LoadDir(dir, MarkdownLoader{})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("期望 2 篇文档，得到 %d", len(docs))
	}
	// 按路径排序：a.md 在 sub/c.md 之前
	if docs[0].Title != "A" || docs[1].Title != "C" {
		t.Errorf("文档顺序错误: %q, %q", docs[0].Title, docs[1].Title)
	}
}

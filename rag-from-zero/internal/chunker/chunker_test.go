package chunker

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/0226zy/magents/rag-from-zero/internal/loader"
)

func doc(content string) loader.Document {
	return loader.Document{Content: content, Source: "test.md", Title: "文档标题"}
}

func TestSplitByHeadings(t *testing.T) {
	c := &Chunker{MaxSize: 512, Overlap: 50}
	d := doc("# 标题\n\n前言内容。\n\n## 第一章\n\n第一章内容。\n\n## 第二章\n\n第二章内容。\n")
	chunks := c.Split(d)
	if len(chunks) != 3 {
		t.Fatalf("期望 3 个 chunk，得到 %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Heading != "文档标题" {
		t.Errorf("前言 chunk Heading = %q", chunks[0].Heading)
	}
	if chunks[1].Heading != "第一章" || chunks[2].Heading != "第二章" {
		t.Errorf("章节 Heading 错误: %q, %q", chunks[1].Heading, chunks[2].Heading)
	}
	for i, ch := range chunks {
		if ch.Index != i {
			t.Errorf("chunk[%d].Index = %d", i, ch.Index)
		}
		if ch.Source != "test.md" {
			t.Errorf("chunk[%d].Source = %q", i, ch.Source)
		}
	}
}

func TestSplitLongSection(t *testing.T) {
	c := &Chunker{MaxSize: 100, Overlap: 10}
	long := strings.Repeat("这是一句很长的话用来填充内容。", 10) // 300 字符
	d := doc("## 长章节\n\n" + long)
	chunks := c.Split(d)
	if len(chunks) < 2 {
		t.Fatalf("长章节应被切成多块，得到 %d 块", len(chunks))
	}
	for _, ch := range chunks {
		if utf8.RuneCountInString(ch.Text) > 100 {
			t.Errorf("chunk 超长: %d 字符", utf8.RuneCountInString(ch.Text))
		}
		if ch.Heading != "长章节" {
			t.Errorf("Heading = %q", ch.Heading)
		}
	}
	// 相邻块应有重叠内容（后一块的开头应出现在前一块中）
	if !strings.Contains(chunks[0].Text, runesPrefix(chunks[1].Text, 10)) {
		t.Error("相邻 chunks 之间缺少重叠")
	}
}

func TestEmptyDocument(t *testing.T) {
	c := &Chunker{MaxSize: 512, Overlap: 50}
	if chunks := c.Split(doc("")); len(chunks) != 0 {
		t.Errorf("空文档应产出 0 个 chunk，得到 %d", len(chunks))
	}
}

// runesPrefix 取字符串的前 n 个 rune。
func runesPrefix(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

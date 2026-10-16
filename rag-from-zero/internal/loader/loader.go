package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Document 是解析后的文档对象。
type Document struct {
	Content string
	Source  string // 文件路径
	Title   string
}

// Loader 按扩展名解析文档。v0 只实现 Markdown，接口留扩展位。
type Loader interface {
	Load(path string) (*Document, error)
	Extensions() []string // 如 [".md"]
}

// MarkdownLoader 用 os.ReadFile 读取 Markdown，标题取第一个 `# ` 行。
type MarkdownLoader struct{}

func (MarkdownLoader) Extensions() []string { return []string{".md", ".markdown"} }

func (MarkdownLoader) Load(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("loader: 读取文件 %s: %w", path, err)
	}
	return &Document{
		Content: string(data),
		Source:  path,
		Title:   extractTitle(string(data)),
	}, nil
}

// extractTitle 取第一个 `# ` 行的文本；没有则回退到文件名。
func extractTitle(content string) string {
	for line := range strings.Lines(content) {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "# ") {
			if title := strings.TrimSpace(strings.TrimPrefix(t, "# ")); title != "" {
				return title
			}
		}
	}
	return ""
}

// LoadDir 遍历目录，按扩展名分发到对应 Loader，返回所有文档（按路径排序）。
func LoadDir(dir string, loaders ...Loader) ([]*Document, error) {
	ext2loader := map[string]Loader{}
	for _, l := range loaders {
		for _, ext := range l.Extensions() {
			ext2loader[strings.ToLower(ext)] = l
		}
	}

	var paths []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := ext2loader[strings.ToLower(filepath.Ext(path))]; ok {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("loader: 遍历目录 %s: %w", dir, err)
	}
	sort.Strings(paths)

	docs := make([]*Document, 0, len(paths))
	for _, p := range paths {
		l := ext2loader[strings.ToLower(filepath.Ext(p))]
		doc, err := l.Load(p)
		if err != nil {
			return nil, fmt.Errorf("loader: 解析 %s: %w", p, err)
		}
		if doc.Title == "" {
			doc.Title = strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

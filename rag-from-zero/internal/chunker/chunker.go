package chunker

import (
	"fmt"
	"strings"

	"github.com/0226zy/magents/rag-from-zero/internal/loader"
)

// Chunk 是带元数据的分块。Source/Heading 在生成引用时必须保留。
type Chunk struct {
	ID      string
	Text    string
	Source  string
	Heading string // 所属章节标题，引用时要用
	Index   int    // 在文档中的序号
}

// Chunker 分块器：先按 Markdown 标题切出语义段，超长部分再按段落/句子/字符兜底切，相邻块保留 overlap。
type Chunker struct {
	MaxSize int // 字符数，建议 512 起步
	Overlap int // 约 MaxSize 的 10%
}

// Split 将文档切分为 chunks。
func (c *Chunker) Split(doc loader.Document) []Chunk {
	sections := splitByHeading(doc.Content, doc.Title)

	var chunks []Chunk
	for _, sec := range sections {
		pieces := splitLongText(sec.text, c.MaxSize, c.Overlap)
		for _, p := range pieces {
			text := strings.TrimSpace(p)
			if text == "" {
				continue
			}
			chunks = append(chunks, Chunk{
				ID:      fmt.Sprintf("%s#%d", doc.Source, len(chunks)),
				Text:    text,
				Source:  doc.Source,
				Heading: sec.heading,
				Index:   len(chunks),
			})
		}
	}
	return chunks
}

type section struct {
	heading string
	text    string
}

// splitByHeading 按 `## ` 级标题切分；第一个标题之前的前言归属文档标题。
func splitByHeading(content, docTitle string) []section {
	lines := strings.Split(content, "\n")
	var sections []section
	cur := section{heading: docTitle}
	var body strings.Builder
	flush := func() {
		if t := body.String(); strings.TrimSpace(t) != "" {
			cur.text = t
			sections = append(sections, cur)
		}
		body.Reset()
	}
	for _, line := range lines {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "## ") {
			flush()
			cur = section{heading: strings.TrimSpace(strings.TrimPrefix(t, "## "))}
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	flush()
	return sections
}

// splitLongText 将超长文本切块：优先在 maxSize 之内找段落边界（空行），
// 其次找句子边界（。！？；），最后按字符硬切。相邻块之间保留 overlap 字符。
func splitLongText(text string, maxSize, overlap int) []string {
	runes := []rune(text)
	if len(runes) <= maxSize {
		return []string{text}
	}

	var pieces []string
	start := 0
	for start < len(runes) {
		end := min(start+maxSize, len(runes))
		if end < len(runes) {
			if cut := findCutPoint(runes[start:end]); cut > 0 {
				end = start + cut
			}
		}
		pieces = append(pieces, string(runes[start:end]))
		if end >= len(runes) {
			break
		}
		// 下一块回退 overlap 字符，保证语义连贯
		start = max(end-overlap, start+1)
	}
	return pieces
}

// findCutPoint 在 buf 内从后往前找最佳切分点（空行 > 句子边界），返回相对偏移。
func findCutPoint(buf []rune) int {
	// 段落边界：空行
	for i := len(buf) - 1; i >= len(buf)/2; i-- {
		if buf[i] == '\n' && i+1 < len(buf) && buf[i+1] == '\n' {
			return i + 1
		}
	}
	// 句子边界
	for i := len(buf) - 1; i >= len(buf)/2; i-- {
		switch buf[i] {
		case '。', '！', '？', '；', '.', '!', '?', ';':
			return i + 1
		}
	}
	return 0 // 无合适边界，硬切
}

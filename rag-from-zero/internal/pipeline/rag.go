package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"text/template"
	"time"

	"github.com/0226zy/magents/rag-from-zero/internal/embedder"
	"github.com/0226zy/magents/rag-from-zero/internal/llm"
	"github.com/0226zy/magents/rag-from-zero/internal/vectorstore"
)

// Source 是答案引用的来源（精简自 chunker.Chunk，便于展示）。
type Source struct {
	Source  string  // 文档路径
	Heading string  // 章节标题
	Score   float32 // 相似度分数
}

// Answer 是结构化回答。引用溯源是 RAG 区别于裸模型的核心价值，从第一天就设计上。
type Answer struct {
	Text       string
	Sources    []Source
	RetrieveMs int64 // 检索耗时（含 query 向量化）
	GenerateMs int64 // 生成耗时
}

// Pipeline 串联检索与生成。离线和在线共用同一个 embedder，保证向量空间对齐。
type Pipeline struct {
	embedder embedder.Embedder
	store    *vectorstore.MemoryStore
	llm      llm.LLM
	topK     int
}

func New(e embedder.Embedder, store *vectorstore.MemoryStore, l llm.LLM, topK int) *Pipeline {
	if topK <= 0 {
		topK = 5
	}
	return &Pipeline{embedder: e, store: store, llm: l, topK: topK}
}

// Answer 执行 RAG 主流程：query 向量化 → 检索 Top-K → 组装 Prompt → 调用 LLM。
func (p *Pipeline) Answer(ctx context.Context, question string) (*Answer, error) {
	// 1. query 向量化（与索引用同一个 embedder！）
	t0 := time.Now()
	qVecs, err := p.embedder.Embed(ctx, []string{question})
	if err != nil {
		return nil, fmt.Errorf("pipeline: query 向量化失败: %w", err)
	}
	// 2. 检索 Top-K
	chunks := p.store.Search(qVecs[0], p.topK)
	retrieveMs := time.Since(t0).Milliseconds()

	// 3. 组装 Prompt + 调用 LLM
	prompt, err := renderPrompt(chunks, question)
	if err != nil {
		return nil, err
	}
	t1 := time.Now()
	answerText, err := p.llm.Chat(ctx, []llm.Message{
		{Role: "system", Content: prompt},
		{Role: "user", Content: question},
	})
	if err != nil {
		return nil, fmt.Errorf("pipeline: LLM 生成失败: %w", err)
	}
	generateMs := time.Since(t1).Milliseconds()

	return &Answer{
		Text:       answerText,
		Sources:    extractSources(chunks),
		RetrieveMs: retrieveMs,
		GenerateMs: generateMs,
	}, nil
}

func extractSources(chunks []vectorstore.ScoredChunk) []Source {
	sources := make([]Source, 0, len(chunks))
	for _, c := range chunks {
		sources = append(sources, Source{
			Source:  c.Chunk.Source,
			Heading: c.Chunk.Heading,
			Score:   c.Score,
		})
	}
	return sources
}

// ragPromptTemplate 是 RAG 的 Prompt 模板，是核心资产。用 text/template 管理，便于频繁迭代。
const ragPromptTemplate = `你是一个严谨的问答助手。仅根据以下参考资料回答问题。

要求：
1. 只能使用参考资料中的信息，禁止使用你自己的知识
2. 如果参考资料不足以回答，明确说"根据现有资料无法回答"
3. 回答中标注引用来源，格式：[来源: {{.FileName}} {{.Heading}}]
4. 回答使用中文

参考资料：
{{- range .Chunks}}
---
[来源: {{.FileName}} {{.Heading}}]
{{.Text}}
{{end -}}
---
`

type promptChunk struct {
	FileName string
	Heading  string
	Text     string
}

var tmpl = template.Must(template.New("rag").Parse(ragPromptTemplate))

func renderPrompt(chunks []vectorstore.ScoredChunk, question string) (string, error) {
	data := struct {
		Chunks []promptChunk
	}{}
	for _, c := range chunks {
		data.Chunks = append(data.Chunks, promptChunk{
			FileName: baseName(c.Chunk.Source),
			Heading:  c.Chunk.Heading,
			Text:     c.Chunk.Text,
		})
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("pipeline: 渲染 Prompt: %w", err)
	}
	return buf.String(), nil
}

func baseName(path string) string {
	// 取文件名部分，去掉目录
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

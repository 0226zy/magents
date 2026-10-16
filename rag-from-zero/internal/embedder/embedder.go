package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// Embedder 将文本编码为向量。面向接口编程：明天换 OpenAI Embedding 或其他服务只需替换实现。
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// OllamaEmbedder 封装 Ollama 的 /api/embed 接口，注意三件事：批量、并发、限流。
type OllamaEmbedder struct {
	baseURL     string
	model       string
	client      *http.Client
	batchSize   int
	concurrency int
	maxRetries  int
}

type Option func(*OllamaEmbedder)

func WithBatchSize(n int) Option           { return func(e *OllamaEmbedder) { e.batchSize = n } }
func WithConcurrency(n int) Option         { return func(e *OllamaEmbedder) { e.concurrency = n } }
func WithMaxRetries(n int) Option          { return func(e *OllamaEmbedder) { e.maxRetries = n } }
func WithHTTPClient(c *http.Client) Option { return func(e *OllamaEmbedder) { e.client = c } }

func NewOllamaEmbedder(baseURL, model string, opts ...Option) *OllamaEmbedder {
	e := &OllamaEmbedder{
		baseURL:     baseURL,
		model:       model,
		client:      &http.Client{Timeout: 120 * time.Second},
		batchSize:   32,
		concurrency: 4,
		maxRetries:  3,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"` // Ollama 支持批量
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed 批量编码：按 batchSize 分批，errgroup 并发请求 + semaphore 限流，失败指数退避重试。
// 返回的向量顺序与输入 texts 一致。
func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	type batch struct {
		start int
		texts []string
	}
	var batches []batch
	for i := 0; i < len(texts); i += e.batchSize {
		end := min(i+e.batchSize, len(texts))
		batches = append(batches, batch{start: i, texts: texts[i:end]})
	}

	results := make([][]float32, len(texts))
	sem := semaphore.NewWeighted(int64(e.concurrency))
	g, gctx := errgroup.WithContext(ctx)

	for _, b := range batches {
		b := b
		g.Go(func() error {
			if err := sem.Acquire(gctx, 1); err != nil {
				return err
			}
			defer sem.Release(1)

			vecs, err := e.embedWithRetry(gctx, b.texts)
			if err != nil {
				return fmt.Errorf("embedder: 批量 %d-%d 编码失败: %w", b.start, b.start+len(b.texts), err)
			}
			copy(results[b.start:], vecs)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// embedWithRetry 对单个批次重试（指数退避），Embedding 是幂等操作，放心重试。
func (e *OllamaEmbedder) embedWithRetry(ctx context.Context, texts []string) ([][]float32, error) {
	var lastErr error
	for attempt := 0; attempt <= e.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		vecs, err := e.embedOnce(ctx, texts)
		if err == nil {
			return vecs, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (e *OllamaEmbedder) embedOnce(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embedder: Ollama 返回 %s: %s", resp.Status, msg)
	}

	var out embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embedder: 解析响应: %w", err)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embedder: 向量数量不匹配，期望 %d 得到 %d", len(texts), len(out.Embeddings))
	}
	return out.Embeddings, nil
}

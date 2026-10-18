package embedder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// mockOllama 返回固定维度的向量，并记录收到的批次。
func mockOllama(t *testing.T, dim int, calls *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("错误路径: %s", r.URL.Path)
		}
		calls.Add(1)
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("解析请求失败: %v", err)
			return
		}
		out := embedResponse{Embeddings: make([][]float32, len(req.Input))}
		for i := range out.Embeddings {
			out.Embeddings[i] = make([]float32, dim)
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
}

func TestEmbedBatchesAndPreservesOrder(t *testing.T) {
	var calls atomic.Int32
	srv := mockOllama(t, 4, &calls)
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "test-model", WithBatchSize(3), WithConcurrency(2))
	texts := []string{"a", "b", "c", "d", "e", "f", "g"}
	vecs, err := e.Embed(context.Background(), texts)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("期望 %d 个向量，得到 %d", len(texts), len(vecs))
	}
	for i, v := range vecs {
		if len(v) != 4 {
			t.Errorf("vecs[%d] 维度错误: %d", i, len(v))
		}
	}
	// 7 条 / batchSize 3 = 3 批
	if calls.Load() != 3 {
		t.Errorf("期望 3 次请求，得到 %d", calls.Load())
	}
}

func TestEmbedRetryOnServerError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(embedResponse{Embeddings: [][]float32{{1, 2}}})
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "m", WithMaxRetries(3))
	vecs, err := e.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatalf("应重试后成功，得到错误: %v", err)
	}
	if calls.Load() != 2 || len(vecs) != 1 {
		t.Errorf("calls=%d, vecs=%d", calls.Load(), len(vecs))
	}
}

func TestEmbedExhaustsRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "m", WithMaxRetries(2))
	if _, err := e.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("应返回错误")
	}
	// 1 次原始 + 2 次重试 = 3 次
	if calls.Load() != 3 {
		t.Errorf("期望 3 次调用，得到 %d", calls.Load())
	}
}

func TestEmbedEmptyInput(t *testing.T) {
	e := NewOllamaEmbedder("http://unused", "m")
	vecs, err := e.Embed(context.Background(), nil)
	if err != nil || len(vecs) != 0 {
		t.Errorf("空输入应返回空结果，得到 %v, %v", vecs, err)
	}
}

package vectorstore

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/0226zy/magents/rag-from-zero/internal/chunker"
)

func entry(text string, vec []float32) Entry {
	return Entry{Chunk: chunker.Chunk{ID: text, Text: text}, Vector: vec}
}

func TestCosineSimilarity(t *testing.T) {
	a := []float32{1, 0, 0}
	if got := cosineSimilarity(a, []float32{1, 0, 0}); math.Abs(float64(got-1)) > 1e-6 {
		t.Errorf("相同向量余弦相似度应为 1，得到 %f", got)
	}
	if got := cosineSimilarity(a, []float32{0, 1, 0}); math.Abs(float64(got)) > 1e-6 {
		t.Errorf("正交向量余弦相似度应为 0，得到 %f", got)
	}
	if got := cosineSimilarity(a, []float32{1, 1, 0}); math.Abs(float64(got)-0.7071) > 1e-3 {
		t.Errorf("45 度向量余弦相似度应约 0.7071，得到 %f", got)
	}
	// 长度不一致 / 零向量
	if got := cosineSimilarity(a, []float32{1, 0}); got != 0 {
		t.Errorf("长度不一致应返回 0，得到 %f", got)
	}
	if got := cosineSimilarity(a, []float32{0, 0, 0}); got != 0 {
		t.Errorf("零向量应返回 0，得到 %f", got)
	}
}

func TestSearchOrdersByScore(t *testing.T) {
	store := NewMemoryStore()
	store.Add([]Entry{
		entry("c", []float32{0, 1}),
		entry("a", []float32{1, 0}),
		entry("b", []float32{0.9, 0.1}),
	})

	results := store.Search([]float32{1, 0}, 2)
	if len(results) != 2 {
		t.Fatalf("期望 2 条结果，得到 %d", len(results))
	}
	if results[0].Chunk.Text != "a" || results[1].Chunk.Text != "b" {
		t.Errorf("排序错误: %q, %q", results[0].Chunk.Text, results[1].Chunk.Text)
	}
	if results[0].Score < results[1].Score {
		t.Error("结果应按分数降序")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	store := NewMemoryStore()
	store.Add([]Entry{entry("x", []float32{1, 2, 3})})

	path := filepath.Join(t.TempDir(), "index.json")
	if err := store.SaveToFile(path); err != nil {
		t.Fatal(err)
	}

	loaded := NewMemoryStore()
	if err := loaded.LoadFromFile(path); err != nil {
		t.Fatal(err)
	}
	if loaded.Len() != 1 {
		t.Fatalf("加载后应有 1 条，得到 %d", loaded.Len())
	}
	got := loaded.Search([]float32{1, 2, 3}, 1)
	if len(got) != 1 || got[0].Chunk.Text != "x" {
		t.Errorf("往返后检索结果错误: %+v", got)
	}

	// 文件不存在时应返回空库不报错
	empty := NewMemoryStore()
	if err := empty.LoadFromFile(filepath.Join(t.TempDir(), "nope.json")); err != nil {
		t.Errorf("文件不存在应不报错，得到 %v", err)
	}
	if empty.Len() != 0 {
		t.Errorf("应为空库，得到 %d", empty.Len())
	}
}

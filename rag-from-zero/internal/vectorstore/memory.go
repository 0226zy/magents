package vectorstore

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"

	"github.com/0226zy/magents/rag-from-zero/internal/chunker"
)

// Entry 是一条带向量的分块。
type Entry struct {
	Chunk  chunker.Chunk
	Vector []float32
}

// ScoredChunk 是检索结果：分块 + 相似度分数。
type ScoredChunk struct {
	Chunk chunker.Chunk
	Score float32
}

// MemoryStore 是 v0 内存向量库：全量扫描算余弦相似度。
// 语料几千块时毫秒级，够用；v1 再升级为 sqlite-vec / Qdrant。
type MemoryStore struct {
	mu      sync.RWMutex
	entries []Entry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

// Add 追加写入（不做去重，重复调用会产生重复条目，由调用方保证）。
func (s *MemoryStore) Add(entries []Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, entries...)
}

// Len 返回当前条目数。
func (s *MemoryStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// Search 全量扫描算余弦相似度，排序取 Top-K。
func (s *MemoryStore) Search(query []float32, topK int) []ScoredChunk {
	s.mu.RLock()
	defer s.mu.RUnlock()

	scored := make([]ScoredChunk, 0, len(s.entries))
	for _, e := range s.entries {
		scored = append(scored, ScoredChunk{Chunk: e.Chunk, Score: cosineSimilarity(query, e.Vector)})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	if len(scored) > topK {
		scored = scored[:topK]
	}
	return scored
}

// cosineSimilarity 向量检索的数学本质就是余弦相似度排序。
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denominator := math.Sqrt(normA) * math.Sqrt(normB)
	if denominator == 0 {
		return 0
	}
	return float32(dot / denominator)
}

// ---------- v0 持久化：JSON 落盘，server 启动时加载 ----------

// indexFile 是落盘文件格式。
type indexFile struct {
	Entries []Entry `json:"entries"`
}

// SaveToFile 将索引序列化到 path（JSON）。
func (s *MemoryStore) SaveToFile(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.Marshal(indexFile{Entries: s.entries})
	if err != nil {
		return fmt.Errorf("vectorstore: 序列化索引: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("vectorstore: 写入临时文件: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("vectorstore: 落盘索引: %w", err)
	}
	return nil
}

// LoadFromFile 从 path 加载索引（不存在则返回空库，不报错）。
func (s *MemoryStore) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("vectorstore: 读取索引 %s: %w", path, err)
	}
	var f indexFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("vectorstore: 解析索引 %s: %w", path, err)
	}
	s.mu.Lock()
	s.entries = f.Entries
	s.mu.Unlock()
	return nil
}

// Command ingest 是索引进口（离线）：解析 → 分块 → Embedding → 入库。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/0226zy/magents/rag-from-zero/internal/chunker"
	"github.com/0226zy/magents/rag-from-zero/internal/embedder"
	"github.com/0226zy/magents/rag-from-zero/internal/loader"
	"github.com/0226zy/magents/rag-from-zero/internal/vectorstore"
)

func main() {
	docsDir := flag.String("docs", "data/docs", "原始文档目录")
	indexPath := flag.String("index", "data/index.json", "索引输出文件")
	chunkSize := flag.Int("chunk-size", 512, "分块最大字符数")
	overlap := flag.Int("overlap", 50, "相邻分块重叠字符数")
	ollamaURL := flag.String("ollama", getenvDefault("OLLAMA_BASE_URL", "http://localhost:11434"), "Ollama 地址")
	embedModel := flag.String("embed-model", getenvDefault("OLLAMA_EMBED_MODEL", "bge-m3"), "Embedding 模型")
	flag.Parse()

	t0 := time.Now()

	// 1. 解析
	docs, err := loader.LoadDir(*docsDir, loader.MarkdownLoader{})
	if err != nil {
		log.Fatalf("解析文档失败: %v", err)
	}
	fmt.Printf("解析完成: %d 篇文档\n", len(docs))

	// 2. 分块
	c := &chunker.Chunker{MaxSize: *chunkSize, Overlap: *overlap}
	var chunks []chunker.Chunk
	for _, doc := range docs {
		chunks = append(chunks, c.Split(*doc)...)
	}
	fmt.Printf("分块完成: %d 个 chunks\n", len(chunks))
	if len(chunks) == 0 {
		log.Fatal("没有可分块的内容")
	}

	// 3. Embedding（批量 + 并发，由 embedder 内部处理）
	emb := embedder.NewOllamaEmbedder(*ollamaURL, *embedModel)
	texts := make([]string, len(chunks))
	for i, ch := range chunks {
		texts[i] = ch.Text
	}
	t1 := time.Now()
	vectors, err := emb.Embed(context.Background(), texts)
	if err != nil {
		log.Fatalf("Embedding 失败: %v", err)
	}
	fmt.Printf("Embedding 完成: %d 个向量，耗时 %s\n", len(vectors), time.Since(t1).Round(time.Millisecond))

	// 4. 入库 + 落盘
	store := vectorstore.NewMemoryStore()
	entries := make([]vectorstore.Entry, len(chunks))
	for i := range chunks {
		entries[i] = vectorstore.Entry{Chunk: chunks[i], Vector: vectors[i]}
	}
	store.Add(entries)
	if err := store.SaveToFile(*indexPath); err != nil {
		log.Fatalf("保存索引失败: %v", err)
	}

	fmt.Printf("索引已写入 %s（%d 条），总耗时 %s\n", *indexPath, store.Len(), time.Since(t0).Round(time.Millisecond))
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Command server 是服务入口（在线）：加载索引 → 循环问答（检索 → 生成）。
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/0226zy/magents/rag-from-zero/internal/embedder"
	"github.com/0226zy/magents/rag-from-zero/internal/llm"
	"github.com/0226zy/magents/rag-from-zero/internal/pipeline"
	"github.com/0226zy/magents/rag-from-zero/internal/vectorstore"
)

func main() {
	indexPath := flag.String("index", "data/index.json", "索引文件路径")
	topK := flag.Int("top-k", 5, "检索 Top-K")
	ollamaURL := flag.String("ollama", getenvDefault("OLLAMA_BASE_URL", "http://localhost:11434"), "Ollama 地址")
	embedModel := flag.String("embed-model", getenvDefault("OLLAMA_EMBED_MODEL", "bge-m3"), "Embedding 模型")
	flag.Parse()

	// 1. 加载索引
	store := vectorstore.NewMemoryStore()
	if err := store.LoadFromFile(*indexPath); err != nil {
		log.Fatalf("加载索引失败: %v", err)
	}
	if store.Len() == 0 {
		log.Fatalf("索引为空，请先运行 cmd/ingest 构建索引: %s", *indexPath)
	}
	fmt.Printf("已加载索引: %d 条\n", store.Len())

	// 2. 组装组件（DeepSeek API key 从环境变量 DEEPSEEK_API_KEY 读取）
	llmClient, err := llm.NewFromEnv()
	if err != nil {
		log.Fatalf("初始化 LLM 失败: %v", err)
	}
	emb := embedder.NewOllamaEmbedder(*ollamaURL, *embedModel)
	p := pipeline.New(emb, store, llmClient, *topK)

	// 3. 交互式问答
	fmt.Println("RAG 已就绪，输入问题（exit / quit 退出）")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}
		question := strings.TrimSpace(scanner.Text())
		if question == "" {
			continue
		}
		if question == "exit" || question == "quit" {
			break
		}

		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		ans, err := p.Answer(ctx, question)
		cancel()
		if err != nil {
			fmt.Printf("错误: %v\n", err)
			continue
		}

		fmt.Printf("\n%s\n", ans.Text)
		fmt.Printf("\n[来源]\n")
		for i, s := range ans.Sources {
			fmt.Printf("  %d. %s · %s (score=%.3f)\n", i+1, s.Source, s.Heading, s.Score)
		}
		fmt.Printf("[耗时] 检索 %dms，生成 %dms\n", ans.RetrieveMs, ans.GenerateMs)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("读取输入失败: %v", err)
	}
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

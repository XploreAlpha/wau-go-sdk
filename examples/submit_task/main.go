// 示例:提交一个 L4 任务(真发 A2A)
//
// 跑法:
//
//	cd examples/submit_task && go run main.go "What is the capital of France?"
//
// 期望:kernel 选 Whis,返 "Paris"
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	wau "github.com/wau/wau-go-sdk"
)

func main() {
	prompt := "What is the capital of France?"
	if len(os.Args) > 1 {
		prompt = os.Args[1]
	}

	c, err := wau.New("http://localhost:18400",
		wau.WithTimeout(30*time.Second),
	)
	if err != nil {
		log.Fatalf("wau.New: %v", err)
	}
	defer c.Close()

	resp, err := c.Tasks().Submit(context.Background(), wau.SubmitRequest{
		Prompt:    prompt,
		TimeoutMs: 30000,
	})
	if err != nil {
		log.Fatalf("Submit: %v", err)
	}
	fmt.Printf("✅ 状态: %s\n", resp.Status)
	fmt.Printf("🤖 选中 agent: %s (score=%.2f)\n", resp.SelectedAgent, resp.Score)
	fmt.Printf("📊 L3 决策: %dms | A2A 调用: %dms\n",
		resp.Decision.DecisionTimeMs, resp.A2ACallMs)
	fmt.Printf("💬 响应: %v\n", resp.Response)
}

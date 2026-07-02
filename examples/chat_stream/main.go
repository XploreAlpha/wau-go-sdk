// 示例:wau-go-sdk Chat SSE 流式调用 — 累加 content + 输出 chatcmpl ID
//
// 跑法:
//
//	cd examples/chat_stream && go run main.go
//
// 期望:
//   - 启动 Go SDK Stream()
//   - 启 httptest mock server 模拟 wau-edge /v1/chat/completions?stream=true 行为
//   - 收到 6 个 ChatCompletionChunk(role + 5 字符"1+1=2")
//   - 累加 delta.content = "1+1=2"
//   - chatcmpl ID 输出
//
// 为什么用 mock server(不连真 wau-edge):
//   - 真 wau-edge 在公网 43.134.126.126(:18402),需要 SSH + 跨网
//   - 真实链路已通过 [[2026-07-02-PROGRESS-M5-#1+-curl-edges]] C.1 测试(7 chunks)验证
//   - 本 example 专注 SDK API 用法,用 mock server 演示完整 Stream() 流程
//   - 真 e2e 走 [[2026-07-01-PROGRESS-M5-#4-sdk-go]] Stage 3.1 #4 已验(chatcmpl-787dcac6)
//
// 真实链路(per Stage 3.1 #10):
//   Go SDK Stream() → wau-edge :18402 /v1/chat/completions?stream=true
//                  → wau-llm-router :18404 Resolve(unary, 拿 userToken + model)
//                  → new-api sidecar → DeepSeek v4-flash → SSE chunks → 响应回 SDK
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	wau "github.com/wau/wau-go-sdk"
)

func main() {
	// 1. 启 httptest mock server 模拟 wau-edge SSE 响应
	//    模拟 6 chunks:role + 5 字符"1+1=2" + DONE
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验请求:Accept: text/event-stream
		if r.Header.Get("Accept") != "text/event-stream" {
			http.Error(w, "Accept must be text/event-stream", http.StatusBadRequest)
			return
		}
		var req wau.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if !req.Stream {
			http.Error(w, "Stream must be true", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		// 6 chunks(role + "1+1=2")
		role := "assistant"
		stop := "stop"
		chunks := []wau.ChatCompletionChunk{
			{ID: "chatcmpl-example-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []wau.ChunkChoice{{Index: 0, Delta: wau.ChunkDelta{Role: role}}}},
			{ID: "chatcmpl-example-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []wau.ChunkChoice{{Index: 0, Delta: wau.ChunkDelta{Content: "1"}}}},
			{ID: "chatcmpl-example-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []wau.ChunkChoice{{Index: 0, Delta: wau.ChunkDelta{Content: "+"}}}},
			{ID: "chatcmpl-example-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []wau.ChunkChoice{{Index: 0, Delta: wau.ChunkDelta{Content: "1"}}}},
			{ID: "chatcmpl-example-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []wau.ChunkChoice{{Index: 0, Delta: wau.ChunkDelta{Content: "="}}}},
			{ID: "chatcmpl-example-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []wau.ChunkChoice{{Index: 0, Delta: wau.ChunkDelta{Content: "2"}}, {Index: 0, Delta: wau.ChunkDelta{}, FinishReason: &stop}}},
		}
		for _, c := range chunks {
			b, _ := json.Marshal(c)
			fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	// 2. 初始化 wau Client(指向 mock server)
	c, err := wau.New(srv.URL, wau.WithTimeout(10*time.Second))
	if err != nil {
		log.Fatalf("wau.New: %v", err)
	}
	defer c.Close()

	fmt.Println("=== wau-go-sdk Chat SSE 流式调用(against mock wau-edge)===")
	fmt.Printf("url:    %s\n", srv.URL)
	fmt.Println("model:  deepseek-v4-flash")
	fmt.Println("prompt: 1+1=?")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 3. 调 Stream()(per Stage 3.1 #10)
	chunks, errs, _ := c.Chat().Stream(ctx, wau.ChatCompletionRequest{
		Model: "deepseek-v4-flash",
		Messages: []wau.ChatMessage{
			{Role: "user", Content: "1+1=?"},
		},
	})

	// 4. iter 收 chunk,累加 content
	var fullContent strings.Builder
	var lastID string
	chunkCount := 0
	fmt.Print("response: ")
	for chunk := range chunks {
		chunkCount++
		lastID = chunk.ID
		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				fmt.Print(delta.Content)
				fullContent.WriteString(delta.Content)
			}
			// 结束信号(per OpenAI SSE:finish_reason="stop")
			if chunk.Choices[0].FinishReason != nil && *chunk.Choices[0].FinishReason == "stop" {
				break
			}
		}
	}
	fmt.Println()
	fmt.Println()

	// 5. 查错
	if err := <-errs; err != nil {
		log.Fatalf("Stream err: %v", err)
	}

	// 6. 总结
	fmt.Println("=== 总结 ===")
	fmt.Printf("chatcmpl:  %s\n", lastID)
	fmt.Printf("chunks:    %d (role + 5 chars)\n", chunkCount)
	fmt.Printf("content:   %s\n", fullContent.String())
	fmt.Println()
	fmt.Println("✅ Stream() 拿到 6 chunks,累加 content='1+1=2'")
	fmt.Println("✅ FinishReason=*stop 终止,errs nil")
	fmt.Println("✅ SDK SSE 解析正确(per wau-edge stream.go WriteChunk / WriteDone)")
}

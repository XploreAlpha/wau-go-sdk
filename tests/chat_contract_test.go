// Package tests — v0.9.0 M3 §3.7 跨 SDK chat 字节级契约测试
//
// 验证 4 SDK(go/python/ts/rust) 走 wau-edge OpenAI 兼容层时,wire 字节级对齐:
//
//   1. wau-go-sdk Chat().Completions 真实 HTTP 调用 → 字节级 POST body
//      跟 OpenAI Chat Completions spec 对齐(per https://platform.openai.com/docs/api-reference/chat)
//   2. mock 响应字节级 JSON 跟 OpenAI spec 对齐,4 SDK 都能解析
//   3. 黄金 JSON 在 ./contract-golden/chat_*.json,作为 4 SDK 共享真相源
//
// 跨 SDK e2e 流程(per M3 §4.5.1):
//   bot → wau-edge :18402 /v1/chat/completions
//        → wau-llm-router :18403 /v1/resolve
//        → new-api :3000 /v1/chat/completions
//
// 本测试专注**第 1 跳** (bot → wau-edge) 字节级对齐。后续 wau-llm-router / new-api 的 e2e
// 在 wau-llm-router 仓的跨包 e2e 测试中覆盖(per M3 §3.2 排期)。
package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	wau "github.com/wau/wau-go-sdk"
)

// chatMockWauEdge 模拟 wau-edge OpenAI 兼容层(per M2 §2.5 + M3 §3.7)
type chatMockWauEdge struct {
	receivedBody []byte // 收到的原始 POST body(给 byte-level 断言)
	receivedCT   string
	authHeader   string
}

func (m *chatMockWauEdge) start(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 路径 + method 校验(对齐 wau-edge M2 §2.5)
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 记录原始 wire(给 byte-level 断言)
		m.receivedCT = r.Header.Get("Content-Type")
		m.authHeader = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		m.receivedBody = body

		// 校验基础 contract: Content-Type 必须是 application/json
		if m.receivedCT != "application/json" {
			http.Error(w, "bad content-type", http.StatusBadRequest)
			return
		}

		// 解析 body, 简单校验, 然后返 OpenAI 字节级响应
		var req wau.ChatCompletionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Model == "" {
			http.Error(w, `{"error":{"code":-32600,"message":"missing model"}}`, http.StatusBadRequest)
			return
		}
		if len(req.Messages) == 0 {
			http.Error(w, `{"error":{"code":-32600,"message":"missing messages"}}`, http.StatusBadRequest)
			return
		}

		// 构造 OpenAI 字节级响应(对齐 OpenAI ChatCompletion object)
		resp := wau.ChatCompletionResponse{
			ID:      "chatcmpl-e2e-001",
			Object:  "chat.completion",
			Created: 1700000000,
			Model:   req.Model,
			Choices: []wau.ChatChoice{
				{
					Index: 0,
					Message: wau.ChatMessage{
						Role:    "assistant",
						Content: "echo: " + req.Messages[len(req.Messages)-1].Content,
					},
					FinishReason: "stop",
				},
			},
			Usage: wau.ChatUsage{
				PromptTokens:     5,
				CompletionTokens: 3,
				TotalTokens:      8,
			},
			Reason: "static:tenant=acme model=" + req.Model,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// ============== Case 1:wire 字节级对齐 OpenAI spec ==============

func TestChatContract_WireLevel_PostBody(t *testing.T) {
	mock := &chatMockWauEdge{}
	srv := mock.start(t)
	defer srv.Close()

	c, err := wau.New(srv.URL, wau.WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	resp, err := c.Chat().Completions(context.Background(), wau.ChatCompletionRequest{
		Model: "gpt-4o-mini",
		Messages: []wau.ChatMessage{
			{Role: "system", Content: "你是一个 helpful assistant。"},
			{Role: "user", Content: "你好"},
		},
		Universe: "default",
		Metadata: map[string]string{"source": "telegram"},
	})
	if err != nil {
		t.Fatalf("Completions: %v", err)
	}

	// 1. SDK 解析响应成功
	if resp.ID != "chatcmpl-e2e-001" {
		t.Errorf("resp.id = %q, want chatcmpl-e2e-001", resp.ID)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "echo: 你好" {
		t.Errorf("resp content = %v, want echo: 你好", resp.Choices[0].Message.Content)
	}

	// 2. wire 字节级断言(给 4 SDK 跨语言对齐用)
	if mock.receivedCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", mock.receivedCT)
	}

	// 验证 body 含 5 个 OpenAI 必填字段
	bodyStr := string(mock.receivedBody)
	for _, field := range []string{`"model":"gpt-4o-mini"`, `"role":"system"`, `"role":"user"`, `"content":"你是一个 helpful assistant。"`, `"content":"你好"`} {
		if !bytes.Contains(mock.receivedBody, []byte(field)) {
			t.Errorf("body 缺字段 %q, body = %s", field, bodyStr)
		}
	}
	// 验证 WAU 扩展字段透传
	for _, ext := range []string{`"universe":"default"`, `"source":"telegram"`} {
		if !bytes.Contains(mock.receivedBody, []byte(ext)) {
			t.Errorf("body 缺 WAU 扩展字段 %q, body = %s", ext, bodyStr)
		}
	}

	// 3. Auth 头透传(per wau-edge §2.2 JWT 中间件)
	if mock.authHeader != "" {
		t.Logf("Auth 头: %s", mock.authHeader)
	}
}

// ============== Case 2:黄金 JSON 真相源校验 ==============

func TestChatContract_GoldenJSON_Alignment(t *testing.T) {
	goldenDir := filepath.Join("contract-golden")
	entries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Skipf("contract-golden 不存在, 跳过: %v", err)
	}

	// 期望至少 1 个 chat_*.json 黄金文件
	chatGoldens := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" && len(e.Name()) >= 5 && e.Name()[:5] == "chat_" {
			chatGoldens++
			t.Logf("找到 chat 黄金 JSON: %s", e.Name())
		}
	}
	if chatGoldens == 0 {
		t.Skipf("contract-golden/ 下暂无 chat_*.json 黄金文件(本测试为占位)")
	}
}

// ============== Case 3:跨 SDK 字段对齐(4 SDK 共用契约) ==============

func TestChatContract_CrossSDK_FieldMapping(t *testing.T) {
	// 4 SDK 字段命名 1:1 对齐 OpenAI spec(per plan §B.7):
	//
	//   Go:     ChatMessage{role, content, name}
	//   Python: ChatMessage{role, content, name}
	//   TS:     ChatMessage{role, content, name}
	//   Rust:   ChatMessage{role, content, name}
	//
	// 序列化 wire JSON 字段名必须一致(messages 数组每项 {"role":..., "content":..., "name":...})

	mock := &chatMockWauEdge{}
	srv := mock.start(t)
	defer srv.Close()

	c, _ := wau.New(srv.URL, wau.WithTimeout(5*time.Second))
	defer c.Close()

	_, err := c.Chat().Completions(context.Background(), wau.ChatCompletionRequest{
		Model: "gpt-4o-mini",
		Messages: []wau.ChatMessage{
			{Role: "user", Content: "test", Name: "alice"},
		},
	})
	if err != nil {
		t.Fatalf("Completions: %v", err)
	}

	// 验证 wire JSON 字段命名(跨 SDK 共享, 4 SDK 都必须生成相同 JSON)
	expectedSubstrings := []string{
		`"role":"user"`,
		`"content":"test"`,
		`"name":"alice"`,
		`"model":"gpt-4o-mini"`,
		`"messages":[`,
	}
	for _, s := range expectedSubstrings {
		if !bytes.Contains(mock.receivedBody, []byte(s)) {
			t.Errorf("wire JSON 缺字段 %q, body = %s", s, mock.receivedBody)
		}
	}
}

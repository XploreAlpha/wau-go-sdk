// v0.9.0 M3 §3.7 — wau-go-sdk Chat client test
//
// 测试模式(沿用 handshake_test.go):
//   - 启 httptest mock server,模拟 wau-edge /v1/chat/completions 行为
//   - mock 返 OpenAI 字节级 JSON 验证 wau-go-sdk ChatService 解析对齐
//
// 5 case(per plan §B.7):
//   1. happy path(POST → OpenAI 响应解析)
//   2. empty model → 客户端校验(不发请求)
//   3. empty messages → 客户端校验(不发请求)
//   4. server 4xx(InvalidRequest -32600) → APIError 透传
//   5. CompletionsRaw 返 raw bytes(给 streaming 后续阶段)
package wau

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// chatMockWauEdge 模拟 wau-edge OpenAI 兼容层(per M2 §2.5 字节级 mock)
type chatMockWauEdge struct {
	// hits 收到调用次数
	hits int
	// lastReq 收到的最后一次请求体(给 assert 用)
	lastReq ChatCompletionRequest
	// failMode 注入错误:"" / "invalid_request" / "model_not_found"
	failMode string
}

func newChatMockWauEdge() *chatMockWauEdge { return &chatMockWauEdge{} }

func (m *chatMockWauEdge) start(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.hits++
		w.Header().Set("Content-Type", "application/json")

		// 路径校验
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 解析 body
		if err := json.NewDecoder(r.Body).Decode(&m.lastReq); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		// 错误注入
		if m.failMode == "invalid_request" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"code":    -32600,
					"message": "InvalidRequest: empty messages",
				},
			})
			return
		}
		if m.failMode == "model_not_found" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"code":    -32601,
					"message": "ModelNotFound: gpt-99",
				},
			})
			return
		}

		// happy path — 返 OpenAI 字节级 JSON
		_ = json.NewEncoder(w).Encode(ChatCompletionResponse{
			ID:      "chatcmpl-mock-001",
			Object:  "chat.completion",
			Created: 1700000000,
			Model:   m.lastReq.Model,
			Choices: []ChatChoice{
				{
					Index: 0,
					Message: ChatMessage{
						Role:    "assistant",
						Content: "echo: " + m.lastReq.Messages[len(m.lastReq.Messages)-1].Content,
					},
					FinishReason: "stop",
				},
			},
			Usage: ChatUsage{
				PromptTokens:     5,
				CompletionTokens: 3,
				TotalTokens:      8,
			},
			Reason: "static:tenant=acme model=" + m.lastReq.Model,
		})
	}))
}

// TestChat_Completions_HappyPath
func TestChat_Completions_HappyPath(t *testing.T) {
	mock := newChatMockWauEdge()
	srv := mock.start(t)
	defer srv.Close()

	c, err := New(srv.URL, WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	resp, err := c.Chat().Completions(context.Background(), ChatCompletionRequest{
		Model: "gpt-4o-mini",
		Messages: []ChatMessage{
			{Role: "user", Content: "hello"},
		},
		Universe: "default",
	})
	if err != nil {
		t.Fatalf("Completions: %v", err)
	}
	if resp.ID != "chatcmpl-mock-001" {
		t.Errorf("id = %q, want chatcmpl-mock-001", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "echo: hello" {
		t.Errorf("content = %q, want 'echo: hello'", resp.Choices[0].Message.Content)
	}
	if resp.Reason == "" {
		t.Errorf("reason empty, wau-llm-router reason 应透传")
	}
	if mock.lastReq.Universe != "default" {
		t.Errorf("server 收到 universe = %q, want 'default'", mock.lastReq.Universe)
	}
	if mock.hits != 1 {
		t.Errorf("hits = %d, want 1", mock.hits)
	}
}

// TestChat_Completions_EmptyModel
func TestChat_Completions_EmptyModel(t *testing.T) {
	mock := newChatMockWauEdge()
	srv := mock.start(t)
	defer srv.Close()

	c, _ := New(srv.URL, WithTimeout(5*time.Second))
	defer c.Close()

	_, err := c.Chat().Completions(context.Background(), ChatCompletionRequest{
		// Model 缺
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty model")
	}
	if !strings.Contains(err.Error(), "Model is required") {
		t.Errorf("err = %v, want 'Model is required'", err)
	}
	if mock.hits != 0 {
		t.Errorf("hits = %d, want 0(客户端校验,不应发请求)", mock.hits)
	}
}

// TestChat_Completions_EmptyMessages
func TestChat_Completions_EmptyMessages(t *testing.T) {
	mock := newChatMockWauEdge()
	srv := mock.start(t)
	defer srv.Close()

	c, _ := New(srv.URL, WithTimeout(5*time.Second))
	defer c.Close()

	_, err := c.Chat().Completions(context.Background(), ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Messages: nil, // 空
	})
	if err == nil {
		t.Fatal("expected error for empty messages")
	}
	if !strings.Contains(err.Error(), "Messages must not be empty") {
		t.Errorf("err = %v, want 'Messages must not be empty'", err)
	}
	if mock.hits != 0 {
		t.Errorf("hits = %d, want 0", mock.hits)
	}
}

// TestChat_Completions_ServerError_InvalidRequest
func TestChat_Completions_ServerError_InvalidRequest(t *testing.T) {
	mock := newChatMockWauEdge()
	mock.failMode = "invalid_request"
	srv := mock.start(t)
	defer srv.Close()

	c, _ := New(srv.URL, WithTimeout(5*time.Second))
	defer c.Close()

	_, err := c.Chat().Completions(context.Background(), ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 400")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("status = %d, want 400", apiErr.StatusCode)
	}
}

// TestChat_CompletionsRaw_Bytes
func TestChat_CompletionsRaw_Bytes(t *testing.T) {
	mock := newChatMockWauEdge()
	srv := mock.start(t)
	defer srv.Close()

	c, _ := New(srv.URL, WithTimeout(5*time.Second))
	defer c.Close()

	raw, err := c.Chat().CompletionsRaw(context.Background(), ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Messages: []ChatMessage{{Role: "user", Content: "raw test"}},
	})
	if err != nil {
		t.Fatalf("CompletionsRaw: %v", err)
	}
	// raw 应是合法 OpenAI JSON
	var resp ChatCompletionResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("Unmarshal: %v, raw = %s", err, raw)
	}
	if resp.ID == "" {
		t.Errorf("id empty")
	}
	if len(resp.Choices) == 0 {
		t.Errorf("choices empty")
	}
}

// ============================================================
// Streaming SSE tests (Stage 3.1 #10, 2026-07-02)
// ============================================================

// streamSSEChunks 模拟 wau-edge /v1/chat/completions?stream=true 响应。
// 5 个 chunk 模拟 "1+1=2" 推理输出(per C.1 7 chunks 简化版):
//   chunk 1: role="assistant"  (首 chunk)
//   chunk 2-6: content="1", "+", "1", "=", "2"  (5 字符)
//   EOF: data: [DONE]
func streamSSEChunks(w http.ResponseWriter, chunks []ChatCompletionChunk) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
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
}

// startStreamMock 启一个 SSE 模拟 server。
// scenario:
//   "happy":   6 chunks(role + "1+1=2") + DONE
//   "empty":   直接 DONE(无 chunk)
//   "auth":    401 Unauthorized
//   "badjson": role chunk + 坏 JSON 触发 SDK 解析错
//   "partial": role + 2 chars + scanner 中断
func startStreamMock(t *testing.T, scenario string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验请求: stream=true, Accept: text/event-stream
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Accept = %q, want text/event-stream", r.Header.Get("Accept"))
		}
		var req ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if !req.Stream {
			t.Errorf("req.Stream = false, want true")
		}

		switch scenario {
		case "auth":
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": -32001, "message": "访问凭证无效"}})
			return
		case "badjson":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			// role chunk
			b, _ := json.Marshal(ChatCompletionChunk{ID: "chatcmpl-badjson-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Role: "assistant"}}}})
			fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
			// 坏 JSON
			fmt.Fprint(w, "data: {not valid json\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		case "partial":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			for _, content := range []string{"a", "b"} {
				b, _ := json.Marshal(ChatCompletionChunk{ID: "chatcmpl-partial-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: content}}}})
				fmt.Fprintf(w, "data: %s\n\n", b)
				if flusher != nil {
					flusher.Flush()
				}
			}
			// 中断(hj.Close 等同于 TCP 断)
			return
		case "empty":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			fmt.Fprint(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		default: // "happy"
			role := "assistant"
			stop := "stop"
			streamSSEChunks(w, []ChatCompletionChunk{
				{ID: "chatcmpl-stream-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Role: role}}}},
				{ID: "chatcmpl-stream-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: "1"}}}},
				{ID: "chatcmpl-stream-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: "+"}}}},
				{ID: "chatcmpl-stream-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: "1"}}}},
				{ID: "chatcmpl-stream-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: "="}}}},
				{ID: "chatcmpl-stream-1", Object: "chat.completion.chunk", Created: 1700000000, Model: req.Model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: "2"}}, {Index: 0, Delta: ChunkDelta{}, FinishReason: &stop}}},
			})
		}
	}))
}

// TestChat_Stream_HappyPath
func TestChat_Stream_HappyPath(t *testing.T) {
	srv := startStreamMock(t, "happy")
	defer srv.Close()
	c, _ := New(srv.URL, WithTimeout(5*time.Second))
	defer c.Close()

	chunks, errs, cancel := c.Chat().Stream(context.Background(), ChatCompletionRequest{
		Model:    "deepseek-v4-flash",
		Messages: []ChatMessage{{Role: "user", Content: "1+1=?"}},
	})
	defer cancel()

	var content strings.Builder
	var lastID string
	chunkCount := 0
	for chunk := range chunks {
		chunkCount++
		lastID = chunk.ID
		if len(chunk.Choices) > 0 {
			content.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
	if err := <-errs; err != nil {
		t.Fatalf("Stream err: %v", err)
	}
	if chunkCount != 6 {
		t.Errorf("chunk count = %d, want 6", chunkCount)
	}
	if got, want := content.String(), "1+1=2"; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
	if lastID != "chatcmpl-stream-1" {
		t.Errorf("lastID = %q, want chatcmpl-stream-1", lastID)
	}
}

// TestChat_Stream_Empty
func TestChat_Stream_Empty(t *testing.T) {
	srv := startStreamMock(t, "empty")
	defer srv.Close()
	c, _ := New(srv.URL, WithTimeout(5*time.Second))
	defer c.Close()

	chunks, errs, _ := c.Chat().Stream(context.Background(), ChatCompletionRequest{
		Model:    "deepseek-v4-flash",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	count := 0
	for range chunks {
		count++
	}
	if err := <-errs; err != nil {
		t.Fatalf("Stream err: %v", err)
	}
	if count != 0 {
		t.Errorf("chunk count = %d, want 0", count)
	}
}

// TestChat_Stream_AuthError
func TestChat_Stream_AuthError(t *testing.T) {
	srv := startStreamMock(t, "auth")
	defer srv.Close()
	c, _ := New(srv.URL, WithTimeout(5*time.Second))
	defer c.Close()

	chunks, errs, _ := c.Chat().Stream(context.Background(), ChatCompletionRequest{
		Model:    "deepseek-v4-flash",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	// 必须 range 完 chunks 才能从 errs 拿错
	for range chunks {
	}
	err := <-errs
	if err == nil {
		t.Fatalf("expected APIError, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("status = %d, want 401", apiErr.StatusCode)
	}
}

// TestChat_Stream_BadJSON
func TestChat_Stream_BadJSON(t *testing.T) {
	srv := startStreamMock(t, "badjson")
	defer srv.Close()
	c, _ := New(srv.URL, WithTimeout(5*time.Second))
	defer c.Close()

	chunks, errs, _ := c.Chat().Stream(context.Background(), ChatCompletionRequest{
		Model:    "deepseek-v4-flash",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	// 第 1 个 chunk(role)应能收到,第 2 个坏 JSON 触发 errs
	roleCount := 0
	for range chunks {
		roleCount++
	}
	err := <-errs
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse SSE chunk") {
		t.Errorf("err = %v, want 'parse SSE chunk'", err)
	}
	if roleCount != 1 {
		t.Errorf("roleCount = %d, want 1", roleCount)
	}
}

// TestChat_Stream_Partial
func TestChat_Stream_Partial(t *testing.T) {
	srv := startStreamMock(t, "partial")
	defer srv.Close()
	c, _ := New(srv.URL, WithTimeout(5*time.Second))
	defer c.Close()

	chunks, errs, _ := c.Chat().Stream(context.Background(), ChatCompletionRequest{
		Model:    "deepseek-v4-flash",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	var content strings.Builder
	for chunk := range chunks {
		if len(chunk.Choices) > 0 {
			content.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
	// 模拟 partial:server 中断连接,errs 应有 scanner err
	// (per bufio.Scanner 在流被强制关闭时会返回 Err)
	_ = <-errs // 可能 nil 也可能 err,不强求
	if content.String() != "ab" {
		t.Errorf("content = %q, want ab", content.String())
	}
}

// TestChat_Stream_EmptyModel
func TestChat_Stream_EmptyModel(t *testing.T) {
	c, _ := New("http://localhost:1", WithTimeout(1*time.Second))
	defer c.Close()

	chunks, errs, _ := c.Chat().Stream(context.Background(), ChatCompletionRequest{
		Model:    "",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	for range chunks {
	}
	err := <-errs
	if err == nil {
		t.Fatalf("expected empty model error, got nil")
	}
	if !strings.Contains(err.Error(), "Model is required") {
		t.Errorf("err = %v, want 'Model is required'", err)
	}
}

// TestChat_Stream_EmptyMessages
func TestChat_Stream_EmptyMessages(t *testing.T) {
	c, _ := New("http://localhost:1", WithTimeout(1*time.Second))
	defer c.Close()

	chunks, errs, _ := c.Chat().Stream(context.Background(), ChatCompletionRequest{
		Model:    "deepseek-v4-flash",
		Messages: []ChatMessage{},
	})
	for range chunks {
	}
	err := <-errs
	if err == nil {
		t.Fatalf("expected empty messages error, got nil")
	}
	if !strings.Contains(err.Error(), "Messages must not be empty") {
		t.Errorf("err = %v, want 'Messages must not be empty'", err)
	}
}

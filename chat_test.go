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

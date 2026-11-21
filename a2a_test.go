package wau

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 实测抓到的 A2A v1.0 响应体(2026-11-19,hermes-shop01:9901)。
const realA2ATaskResponse = `{
  "jsonrpc": "2.0",
  "id": "probe-1",
  "result": {
    "task": {
      "id": "task-281b4d78ec544907",
      "contextId": "ctx-e68fad6cada24e31",
      "status": {
        "state": "TASK_STATE_COMPLETED",
        "timestamp": "2026-11-19T12:15:22.356Z",
        "message": {
          "role": "ROLE_AGENT",
          "parts": [{"text": "通了", "mediaType": "text/plain"}],
          "messageId": "5552e974436e48e39fbe3cc2f4121d95",
          "contextId": "ctx-e68fad6cada24e31"
        }
      },
      "artifacts": [
        {"artifactId": "b63889df32b04c06969bdf98583c2",
         "parts": [{"text": "通了", "mediaType": "text/plain"}]}
      ]
    }
  }
}`

func newA2ATestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// ★ 2026-11-19:A2A v1.0 客户端回归测试。

func TestA2A_SendMessageSetsVersionHeader(t *testing.T) {
	var gotHeader, gotAuth, gotBody string
	srv := newA2ATestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("A2A-Version")
		gotAuth = r.Header.Get("Authorization")
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)
		gotBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(realA2ATaskResponse))
	})

	c, err := New("http://mock-kernel:18400")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.A2A().SendMessage(context.Background(), srv.URL+"/", "你好", "peer-tok", ""); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if gotHeader != "1.0" {
		t.Errorf("A2A-Version = %q, want 1.0", gotHeader)
	}
	if gotAuth != "Bearer peer-tok" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer peer-tok")
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if body["method"] != "SendMessage" {
		t.Errorf("method = %v, want SendMessage", body["method"])
	}
	if body["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0", body["jsonrpc"])
	}
	msg := body["params"].(map[string]any)["message"].(map[string]any)
	if msg["role"] != "user" {
		t.Errorf("role = %v, want user", msg["role"])
	}
	if msg["messageId"] == "" {
		t.Error("messageId must not be empty")
	}
	parts := msg["parts"].([]any)
	if len(parts) != 1 || parts[0].(map[string]any)["text"] != "你好" {
		t.Errorf("parts = %v", parts)
	}
}

func TestA2A_SendTextExtractsTaskBranch(t *testing.T) {
	srv := newA2ATestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(realA2ATaskResponse))
	})
	c, _ := New("http://mock-kernel:18400")
	defer c.Close()

	text, err := c.A2A().SendText(context.Background(), srv.URL+"/", "你好", "", "")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if text != "通了" {
		t.Errorf("text = %q, want %q", text, "通了")
	}
}

func TestA2A_SendTextExtractsMessageBranch(t *testing.T) {
	srv := newA2ATestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"message":{"messageId":"m-1","role":"ROLE_AGENT","parts":[{"text":"message 分支"}]}}}`))
	})
	c, _ := New("http://mock-kernel:18400")
	defer c.Close()

	text, err := c.A2A().SendText(context.Background(), srv.URL+"/", "你好", "", "")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if text != "message 分支" {
		t.Errorf("text = %q", text)
	}
}

func TestA2A_SendTextExtractsLegacyV03(t *testing.T) {
	srv := newA2ATestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"kind":"task","id":"t-1","artifacts":[{"parts":[{"kind":"text","text":"legacy 文本"}]}]}}`))
	})
	c, _ := New("http://mock-kernel:18400")
	defer c.Close()

	text, err := c.A2A().SendText(context.Background(), srv.URL+"/", "你好", "", "")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if text != "legacy 文本" {
		t.Errorf("text = %q", text)
	}
}

func TestA2A_SendMessageReturnsErrorOnJSONRPCError(t *testing.T) {
	srv := newA2ATestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":"1","error":{"code":-32050,"message":"unauthorized"}}`))
	})
	c, _ := New("http://mock-kernel:18400")
	defer c.Close()

	if _, err := c.A2A().SendMessage(context.Background(), srv.URL+"/", "你好", "", ""); err == nil {
		t.Error("expected error on JSON-RPC error response")
	}
}

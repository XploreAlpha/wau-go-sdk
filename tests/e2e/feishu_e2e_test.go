// feishu_e2e_test.go — Feishu Bot SDK mock end-to-end tests (W7.2 closure, 2026-07-09).
//
// 3 cases per spec: success / APIErr / auth_fail.
//
// Strategy:
//   - httptest.NewServer mock handles POST /open-apis/im/v1/messages.
//   - Construct botfeishu.FeishuBot via public constructor NewFeishuBot (dummy appID/secret).
//   - Use reflection to inject a *lark.Client configured with lark.WithOpenBaseUrl(mock.URL)
//     into the unexported `lark` field, and set `running = true` so PostMessage passes the gate.
//   - PostMessage sends JSON body {receive_id, msg_type, content} to /open-apis/im/v1/messages
//     with query receive_id_type=chat_id.
//
// All tests use atomic.Int32 counters and run with 0 creds.
package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	botfeishu "github.com/wau/wau-go-sdk/bot/feishu"
)

// feishuMockServer minimal Feishu OpenAPI mock.
// Handles POST /open-apis/im/v1/messages and (best-effort) tenant_access_token.
type feishuMockServer struct {
	*httptest.Server
	postCalled  atomic.Int32
	tokenCalled atomic.Int32
	lastBody    atomic.Value // raw JSON body
	postStatus  int
	postErrCode int    // Feishu-style code field
	postErrMsg  string // Feishu-style msg field
}

func newFeishuMockServer(postStatus, errCode int, errMsg string) *feishuMockServer {
	m := &feishuMockServer{postStatus: postStatus, postErrCode: errCode, postErrMsg: errMsg}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" && r.Method == http.MethodPost:
			m.tokenCalled.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":               0,
				"msg":                "ok",
				"tenant_access_token": "t-mock-token-001",
				"expire":             7200,
			})
		case r.URL.Path == "/open-apis/im/v1/messages" && r.Method == http.MethodPost:
			m.postCalled.Add(1)
			m.lastBody.Store(string(bodyBytes))
			if m.postStatus != http.StatusOK {
				w.WriteHeader(m.postStatus)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": m.postErrCode,
					"msg":  m.postErrMsg,
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"msg":  "ok",
				"data": map[string]any{
					"message_id": "om_msg_mock_001",
					"chat_id":    "oc_chat_mock_001",
					"msg_type":   "text",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 999, "msg": "not_found"})
		}
	}))
	return m
}

// injectFeishuMockAPI builds a FeishuBot with mock lark.Client + running=true injected.
// Production Start() would attempt to open a WS connection, which fails in unit tests;
// reflection injection is the only path that exercises PostMessage without a live Lark endpoint.
func injectFeishuMockAPI(t *testing.T, mockURL string) *botfeishu.FeishuBot {
	t.Helper()
	b := botfeishu.NewFeishuBot("cli_app_mock", "secret_mock")
	// lark.WithOpenBaseUrl redirects HTTP base URL to our mock server.
	api := lark.NewClient("cli_app_mock", "secret_mock", lark.WithOpenBaseUrl(mockURL))
	setPrivateField(t, b, "lark", api)
	setPrivateField(t, b, "running", true)
	return b
}

// TestFeishuBot_PostMessage_Success — mock returns code=0, bot returns message_id, no error.
func TestFeishuBot_PostMessage_Success(t *testing.T) {
	mock := newFeishuMockServer(http.StatusOK, 0, "ok")
	defer mock.Close()

	b := injectFeishuMockAPI(t, mock.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msgID, err := b.PostMessage(ctx, "oc_chat_mock_001", "hello feishu")
	if err != nil {
		t.Fatalf("PostMessage error = %v, want nil", err)
	}
	if msgID != "om_msg_mock_001" {
		t.Errorf("messageID = %q, want om_msg_mock_001", msgID)
	}
	if got := mock.postCalled.Load(); got != 1 {
		t.Errorf("postCalled = %d, want 1", got)
	}

	// Verify request body shape: must contain receive_id, msg_type, and content (stringified JSON {"text":"..."}).
	body, _ := mock.lastBody.Load().(string)
	if !strings.Contains(body, `"receive_id":"oc_chat_mock_001"`) {
		t.Errorf("body missing receive_id: %s", body)
	}
	if !strings.Contains(body, `"msg_type":"text"`) {
		t.Errorf("body missing msg_type=text: %s", body)
	}
	// Feishu content is a stringified JSON object: {"text":"hello feishu"}.
	if !strings.Contains(body, `\"text\":\"hello feishu\"`) {
		t.Errorf("body missing text content: %s", body)
	}
}

// TestFeishuBot_PostMessage_APIErr — mock returns HTTP 400 + Feishu code != 0.
//
// The lark SDK decodes responses as CodeError and returns an error on either
// non-200 status or `code != 0`. We use HTTP 400 to ensure the SDK returns an error,
// which the bot then surfaces as a user-facing error.
func TestFeishuBot_PostMessage_APIErr(t *testing.T) {
	mock := newFeishuMockServer(http.StatusBadRequest, 230001, "chat_id invalid")
	defer mock.Close()

	b := injectFeishuMockAPI(t, mock.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msgID, err := b.PostMessage(ctx, "oc_chat_bad", "boom")
	if err == nil {
		t.Fatal("PostMessage with API error expected error, got nil")
	}
	if msgID != "" {
		t.Errorf("messageID = %q, want empty on error", msgID)
	}
	if !strings.Contains(err.Error(), "暂时无法回复") {
		t.Errorf("err = %q, want user-facing prefix '暂时无法回复'", err.Error())
	}
	if got := mock.postCalled.Load(); got == 0 {
		t.Errorf("postCalled = 0, want >=1 (mock was not called)")
	}
}

// TestFeishuBot_PostMessage_AuthFail — mock returns 401 unauthorized for token endpoint.
//
// Feishu's auth path: if tenant_access_token endpoint fails with 401, subsequent
// /messages calls will fail. We mock both endpoints to fail with 401 to simulate
// an auth-related error chain.
func TestFeishuBot_PostMessage_AuthFail(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 99991663,
			"msg":  "App secret invalid",
		})
	}))
	defer mock.Close()

	b := injectFeishuMockAPI(t, mock.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := b.PostMessage(ctx, "oc_chat_mock_001", "forbidden")
	if err == nil {
		t.Fatal("PostMessage with 401 expected error, got nil")
	}
	if !strings.Contains(err.Error(), "暂时无法回复") && !strings.Contains(err.Error(), "secret") {
		t.Errorf("err = %q, want user-facing prefix or 'secret'", err.Error())
	}
}

// _ ensures time is referenced (used by context timeout).
var _ = time.Second

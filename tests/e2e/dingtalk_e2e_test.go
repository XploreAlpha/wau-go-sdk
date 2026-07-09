// dingtalk_e2e_test.go — DingTalk Bot SDK mock end-to-end tests (W7.2 closure, 2026-07-09).
//
// 3 cases per spec: success / APIErr / auth_fail.
//
// Strategy:
//   - httptest.NewServer mock accepts the session-webhook POST (the URL DingTalk bot
//     caches per-conversation and uses to reply).
//   - Construct botdingtalk.DingTalkBot via public constructor NewDingTalkBot.
//   - Use reflection to inject the mock URL into the unexported `webhooks` map
//     (keyed by conversationID) and set `running = true`. This skips Stream Mode
//     WS startup (which would otherwise dial a live DingTalk endpoint).
//   - PostMessage calls chatbot.SimpleReplyText → POST JSON to the cached webhook.
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

	botdingtalk "github.com/wau/wau-go-sdk/bot/dingtalk"
)

// dingtalkMockServer minimal DingTalk session-webhook mock.
// The webhook URL is the URL the bot POSTs to; in real DingTalk this is sent in
// the stream callback payload, but for our test we inject it via reflection.
type dingtalkMockServer struct {
	*httptest.Server
	called    atomic.Int32
	lastBody  atomic.Value // captured JSON body
	respCode  int          // HTTP status code
	respErrCd int          // DingTalk errcode field
	respErrMsg string      // DingTalk errmsg field
}

func newDingTalkMockServer(code, errCd int, errMsg string) *dingtalkMockServer {
	m := &dingtalkMockServer{respCode: code, respErrCd: errCd, respErrMsg: errMsg}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		m.called.Add(1)
		m.lastBody.Store(string(bodyBytes))

		if m.respCode != http.StatusOK {
			w.WriteHeader(m.respCode)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errcode": m.respErrCd,
				"errmsg":  m.respErrMsg,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errcode": 0,
			"errmsg":  "ok",
		})
	}))
	return m
}

// injectDingTalkWebhook injects the mock URL into the bot's webhook cache (per conversationID)
// and flips running=true. Without this, PostMessage would short-circuit with
// "no sessionWebhook cached for conversation=...".
func injectDingTalkWebhook(t *testing.T, conversationID, webhookURL string) *botdingtalk.DingTalkBot {
	t.Helper()
	b := botdingtalk.NewDingTalkBot("appkey-mock", "appsecret-mock", "robot-mock")
	// webhooks is map[string]string. We need to set a NEW map (replace via reflection).
	webhooks := map[string]string{conversationID: webhookURL}
	setPrivateField(t, b, "webhooks", webhooks)
	setPrivateField(t, b, "running", true)
	return b
}

// TestDingTalkBot_PostMessage_Success — mock returns 200 errcode=0, bot returns reply id, no error.
func TestDingTalkBot_PostMessage_Success(t *testing.T) {
	mock := newDingTalkMockServer(http.StatusOK, 0, "ok")
	defer mock.Close()

	b := injectDingTalkWebhook(t, "conv-mock-001", mock.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	replyID, err := b.PostMessage(ctx, "conv-mock-001", "hello dingtalk")
	if err != nil {
		t.Fatalf("PostMessage error = %v, want nil", err)
	}
	if !strings.HasPrefix(replyID, "dingtalk.reply.") {
		t.Errorf("replyID = %q, want prefix 'dingtalk.reply.'", replyID)
	}
	if got := mock.called.Load(); got != 1 {
		t.Errorf("called = %d, want 1", got)
	}

	// Verify body shape: DingTalk chatbot SimpleReplyText sends
	//   {"msgtype":"text","text":{"content":"..."}}
	body, _ := mock.lastBody.Load().(string)
	if !strings.Contains(body, `"msgtype":"text"`) {
		t.Errorf("body missing msgtype=text: %s", body)
	}
	if !strings.Contains(body, `"content":"hello dingtalk"`) {
		t.Errorf("body missing text.content: %s", body)
	}
}

// TestDingTalkBot_PostMessage_APIErr — mock returns 500 with errcode != 0.
func TestDingTalkBot_PostMessage_APIErr(t *testing.T) {
	mock := newDingTalkMockServer(http.StatusInternalServerError, 999, "internal error")
	defer mock.Close()

	b := injectDingTalkWebhook(t, "conv-mock-002", mock.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	replyID, err := b.PostMessage(ctx, "conv-mock-002", "boom")
	if err == nil {
		t.Fatal("PostMessage with API error expected error, got nil")
	}
	if replyID != "" {
		t.Errorf("replyID = %q, want empty on error", replyID)
	}
	if !strings.Contains(err.Error(), "暂时无法回复") && !strings.Contains(err.Error(), "internal error") {
		t.Errorf("err = %q, want user-facing prefix or 'internal error'", err.Error())
	}
	if got := mock.called.Load(); got != 1 {
		t.Errorf("called = %d, want 1 (no retry on 500)", got)
	}
}

// TestDingTalkBot_PostMessage_AuthFail — mock returns 401 unauthorized.
//
// DingTalk chatbot session-webhooks are server-issued, so a 401 typically means
// the webhook has expired or been revoked. SDK should surface this as an error.
func TestDingTalkBot_PostMessage_AuthFail(t *testing.T) {
	mock := newDingTalkMockServer(http.StatusUnauthorized, 401, "session webhook expired")
	defer mock.Close()

	b := injectDingTalkWebhook(t, "conv-mock-003", mock.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := b.PostMessage(ctx, "conv-mock-003", "forbidden")
	if err == nil {
		t.Fatal("PostMessage with 401 expected error, got nil")
	}
	if !strings.Contains(err.Error(), "暂时无法回复") && !strings.Contains(err.Error(), "expired") {
		t.Errorf("err = %q, want user-facing prefix or 'expired'", err.Error())
	}
}

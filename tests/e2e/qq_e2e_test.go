// qq_e2e_test.go — QQ Bot SDK mock end-to-end tests (W7.2 closure, 2026-07-09).
//
// 3 cases per spec: success / APIErr / auth_fail.
//
// Strategy:
//   - httptest.NewServer mock handles POST /app/getAppAccessToken and POST /channels/{id}/messages.
//   - Override constant.APIDomain + constant.TokenDomain to redirect HTTP to our mock server.
//   - Construct botqq.QQBot via public constructor NewQQBot.
//   - Use reflection to inject a real botgo openapi.OpenAPI (built with NewOpenAPI + tokenSource)
//     into the unexported `api` field, and set `running = true`. This skips the bot's Start()
//     WS-dial path while still exercising PostMessage's HTTP roundtrip.
//   - PostMessage sends JSON to /channels/{channelID}/messages.
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

	botgo "github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/constant"
	"github.com/tencent-connect/botgo/token"
	botqq "github.com/wau/wau-go-sdk/bot/qq"
)

// qqMockServer minimal QQ OpenAPI mock:
//   - POST /app/getAppAccessToken (token endpoint, on TokenDomain)
//   - POST /channels/{id}/messages (PostMessage endpoint, on APIDomain)
type qqMockServer struct {
	*httptest.Server
	tokenCalled atomic.Int32
	postCalled  atomic.Int32
	lastBody    atomic.Value // captured JSON body
	lastPath    atomic.Value // captured request path
	postStatus  int
	postErrCode int
	postErrMsg  string
}

func newQQMockServer(postStatus, errCode int, errMsg string) *qqMockServer {
	m := &qqMockServer{postStatus: postStatus, postErrCode: errCode, postErrMsg: errMsg}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/app/getAppAccessToken"):
			m.tokenCalled.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":        0,
				"message":     "ok",
				"access_token": "fake-mock-token",
				"expires_in":   "7200",
			})
		case strings.HasPrefix(r.URL.Path, "/channels/") && strings.HasSuffix(r.URL.Path, "/messages"):
			m.postCalled.Add(1)
			m.lastBody.Store(string(bodyBytes))
			m.lastPath.Store(r.URL.Path)
			if m.postStatus != http.StatusOK {
				w.WriteHeader(m.postStatus)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":    m.postErrCode,
					"message": m.postErrMsg,
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "msg-mock-001",
				"channel_id": strings.Split(r.URL.Path, "/")[2],
				"guild_id":   "guild-mock-001",
				"content":    "fake-response",
				"timestamp":  "1700000000",
				"author":     map[string]any{"id": "bot-mock", "username": "fake-bot", "bot": true},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 404, "message": "not_found"})
		}
	}))
	return m
}

// withQQTestDomains temporarily overrides QQ OpenAPI domain globals; restores on cleanup.
func withQQTestDomains(t *testing.T, url string) func() {
	t.Helper()
	origAPI := constant.APIDomain
	origToken := constant.TokenDomain
	constant.APIDomain = url
	constant.TokenDomain = url
	return func() {
		constant.APIDomain = origAPI
		constant.TokenDomain = origToken
	}
}

// injectQQMockAPI builds a QQBot with mock botgo OpenAPI + running=true injected.
// The OpenAPI is real (from botgo.NewOpenAPI) but its HTTP calls are redirected by
// constant.APIDomain / TokenDomain overrides.
func injectQQMockAPI(t *testing.T, mockURL string) *botqq.QQBot {
	t.Helper()
	b := botqq.NewQQBot("qq-app-mock", "client-secret-mock", "bot-token-mock")
	ts := token.NewQQBotTokenSource(&token.QQBotCredentials{
		AppID:     "qq-app-mock",
		AppSecret: "client-secret-mock",
	})
	api := botgo.NewOpenAPI("qq-app-mock", ts)
	setPrivateField(t, b, "api", api)
	setPrivateField(t, b, "running", true)
	return b
}

// TestQQBot_PostMessage_Success — mock returns 200 with message id, bot returns id, no error.
func TestQQBot_PostMessage_Success(t *testing.T) {
	mock := newQQMockServer(http.StatusOK, 0, "ok")
	defer mock.Close()
	restore := withQQTestDomains(t, mock.URL)
	defer restore()

	b := injectQQMockAPI(t, mock.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msgID, err := b.PostMessage(ctx, "ch-mock-001", "hello qq")
	if err != nil {
		t.Fatalf("PostMessage error = %v, want nil", err)
	}
	if msgID != "msg-mock-001" {
		t.Errorf("messageID = %q, want msg-mock-001", msgID)
	}
	if got := mock.postCalled.Load(); got != 1 {
		t.Errorf("postCalled = %d, want 1", got)
	}
	if got := mock.tokenCalled.Load(); got < 1 {
		t.Errorf("tokenCalled = %d, want >=1", got)
	}

	// Verify path & body shape: must hit /channels/ch-mock-001/messages with text content.
	// Note: botgo sends msg_type as a query parameter (?msg_type=7 for text), not in body.
	path, _ := mock.lastPath.Load().(string)
	if !strings.HasSuffix(path, "/channels/ch-mock-001/messages") {
		t.Errorf("path = %q, want suffix /channels/ch-mock-001/messages", path)
	}
	body, _ := mock.lastBody.Load().(string)
	if !strings.Contains(body, `"content":"hello qq"`) {
		t.Errorf("body missing content: %s", body)
	}
}

// TestQQBot_PostMessage_APIErr — mock returns 500 with code=50000 (internal error).
func TestQQBot_PostMessage_APIErr(t *testing.T) {
	mock := newQQMockServer(http.StatusInternalServerError, 50000, "internal server error")
	defer mock.Close()
	restore := withQQTestDomains(t, mock.URL)
	defer restore()

	b := injectQQMockAPI(t, mock.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msgID, err := b.PostMessage(ctx, "ch-mock-002", "boom")
	if err == nil {
		t.Fatal("PostMessage with API error expected error, got nil")
	}
	if msgID != "" {
		t.Errorf("messageID = %q, want empty on error", msgID)
	}
	if !strings.Contains(err.Error(), "暂时无法回复") {
		t.Errorf("err = %q, want user-facing prefix '暂时无法回复'", err.Error())
	}
	if got := mock.postCalled.Load(); got != 1 {
		t.Errorf("postCalled = %d, want 1 (no retry on 500)", got)
	}
}

// TestQQBot_PostMessage_AuthFail — mock returns 401 (token invalid).
//
// QQ's token endpoint and message endpoint share the same OpenAPI client.
// When 401 is returned for token, the SDK surfaces auth error.
func TestQQBot_PostMessage_AuthFail(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    401,
			"message": "invalid access_token",
		})
	}))
	defer mock.Close()
	restore := withQQTestDomains(t, mock.URL)
	defer restore()

	b := injectQQMockAPI(t, mock.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := b.PostMessage(ctx, "ch-mock-003", "forbidden")
	if err == nil {
		t.Fatal("PostMessage with 401 expected error, got nil")
	}
	if !strings.Contains(err.Error(), "暂时无法回复") && !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %q, want user-facing prefix or '401'", err.Error())
	}
}

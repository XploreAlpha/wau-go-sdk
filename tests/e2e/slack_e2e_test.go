// slack_e2e_test.go — Slack Bot SDK mock end-to-end tests (W7.2 closure, 2026-07-09).
//
// 3 cases per spec: success / APIErr / auth_fail.
//
// Strategy:
//   - httptest.NewServer mock handles POST /chat.postMessage.
//   - Construct botslack.SlackBot via public constructor NewSlackBot (dummy tokens).
//   - Use reflection to inject a *slack.Client configured with slack.OptionAPIURL(mock.URL+"/")
//     into the unexported `api` field, and set `running = true` so PostMessage passes the gate.
//   - PostMessage sends a form-encoded body: channel + text, Slack returns JSON `{ok, ts, ...}`.
//
// All tests use atomic.Int32 counters and run with 0 creds (no env vars, no live Slack).
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

	"github.com/slack-go/slack"
	botslack "github.com/wau/wau-go-sdk/bot/slack"
)

// slackMockServer minimal Slack API mock: handles POST /chat.postMessage.
// Counter `postCalled` is asserted to == 1 in all 3 tests.
type slackMockServer struct {
	*httptest.Server
	postCalled atomic.Int32
	lastBody   atomic.Value // string — captured form body for assertions
	postStatus int          // HTTP status code to return (200 / 401 / 500)
	postErr    string       // when non-ok, set Slack-style error string
}

func newSlackMockServer(postStatus int, postErr string) *slackMockServer {
	m := &slackMockServer{postStatus: postStatus, postErr: postErr}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		// Slack SDK form-encodes chat.postMessage; capture body for shape assertions.
		bodyBytes, _ := io.ReadAll(r.Body)
		m.lastBody.Store(string(bodyBytes))

		switch {
		case strings.HasSuffix(r.URL.Path, "/chat.postMessage"):
			m.postCalled.Add(1)
			if m.postStatus != http.StatusOK {
				w.WriteHeader(m.postStatus)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":    false,
					"error": m.postErr,
				})
				return
			}
			_ = r.ParseForm()
			channel := r.Form.Get("channel")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"channel": channel,
				"ts":      "1700000001.000100",
				"message": map[string]any{
					"text": r.Form.Get("text"),
				},
			})
		default:
			// auth.test (called from internal SDK paths if any) and any unknown endpoint
			// just return ok=true so the SDK doesn't crash on incidental calls.
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	return m
}

// injectSlackMockAPI constructs a SlackBot with mock slack.Client + running=true injected.
// This is the only viable strategy given that:
//   - botslack.Start() calls AuthTest against the real Slack API, which fails with dummy tokens.
//   - botslack.PostMessage() short-circuits to "slack bot not running" if api == nil or running == false.
//   - We cannot edit production code (D60 additive, new files only).
func injectSlackMockAPI(t *testing.T, mockURL string) *botslack.SlackBot {
	t.Helper()
	b := botslack.NewSlackBot("xoxb-test-mock", "xapp-test-mock")
	api := slack.New("xoxb-test-mock", slack.OptionAPIURL(mockURL+"/"))
	setPrivateField(t, b, "api", api)
	setPrivateField(t, b, "running", true)
	return b
}

// TestSlackBot_PostMessage_Success — happy path: mock returns 200 ok, bot returns ts, no error.
func TestSlackBot_PostMessage_Success(t *testing.T) {
	mock := newSlackMockServer(http.StatusOK, "")
	defer mock.Close()

	b := injectSlackMockAPI(t, mock.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ts, err := b.PostMessage(ctx, "C-CHAN-001", "hello world")
	if err != nil {
		t.Fatalf("PostMessage error = %v, want nil", err)
	}
	if ts != "1700000001.000100" {
		t.Errorf("ts = %q, want %q", ts, "1700000001.000100")
	}
	if got := mock.postCalled.Load(); got != 1 {
		t.Errorf("postCalled = %d, want 1", got)
	}
	// Verify request body shape: must contain channel=C-CHAN-001 and text=hello world.
	body, _ := mock.lastBody.Load().(string)
	if !strings.Contains(body, "channel=C-CHAN-001") {
		t.Errorf("body missing channel=C-CHAN-001: %s", body)
	}
	if !strings.Contains(body, "text=hello+world") && !strings.Contains(body, "text=hello%20world") {
		t.Errorf("body missing text=hello world: %s", body)
	}
}

// TestSlackBot_PostMessage_APIErr — mock returns 500 + Slack-style error JSON.
func TestSlackBot_PostMessage_APIErr(t *testing.T) {
	mock := newSlackMockServer(http.StatusInternalServerError, "server_error")
	defer mock.Close()

	b := injectSlackMockAPI(t, mock.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ts, err := b.PostMessage(ctx, "C-CHAN-002", "boom")
	if err == nil {
		t.Fatal("PostMessage with API error expected error, got nil")
	}
	if ts != "" {
		t.Errorf("ts = %q, want empty on error", ts)
	}
	// SDK wraps as userFacingErrPrefix on real Slack errors.
	if !strings.Contains(err.Error(), "暂时无法回复") && !strings.Contains(err.Error(), "server_error") {
		t.Errorf("err = %q, want user-facing prefix or 'server_error'", err.Error())
	}
	if got := mock.postCalled.Load(); got != 1 {
		t.Errorf("postCalled = %d, want 1 (no retry on 500)", got)
	}
}

// TestSlackBot_PostMessage_AuthFail — mock returns 401 (invalid_auth).
//
// Slack auth failure surfaces as the SDK's "auth.test" check or HTTP 401 from chat.postMessage;
// in our mock, we just verify the SDK surfaces an error and the call is not retried.
func TestSlackBot_PostMessage_AuthFail(t *testing.T) {
	mock := newSlackMockServer(http.StatusUnauthorized, "invalid_auth")
	defer mock.Close()

	b := injectSlackMockAPI(t, mock.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := b.PostMessage(ctx, "C-CHAN-003", "forbidden")
	if err == nil {
		t.Fatal("PostMessage with 401 expected error, got nil")
	}
	if !strings.Contains(err.Error(), "暂时无法回复") && !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("err = %q, want user-facing prefix or 'invalid_auth'", err.Error())
	}
	if got := mock.postCalled.Load(); got != 1 {
		t.Errorf("postCalled = %d, want 1 (no retry on 401)", got)
	}
}

package botwebhook

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wau/wau-go-sdk/bot/common"
)

// TestWebhookBot_New 验证 New(addr, builder)。
func TestWebhookBot_New(t *testing.T) {
	b := botcommon.NewBuilder().WithTenant("acme")
	bot := New(":8080", b)
	if bot == nil {
		t.Fatal("New returned nil")
	}
	if bot.Addr != ":8080" {
		t.Errorf("Addr = %q", bot.Addr)
	}
	if bot.Tenant != "acme" {
		t.Errorf("Tenant = %q", bot.Tenant)
	}
}

// TestWebhookBot_StartStop Stage 1 实装:Start 会启 HTTP server 在 :0。测 Start 用 TestStart_StartsServer。
func TestWebhookBot_StartStop(t *testing.T) {
	t.Skip("Stage 1: Start 启 HTTP server,改用 TestStart_StartsServer + TestStop_GracefulShutdown")
}

// TestWebhookBot_Builder 验证 builder 字段全部透传。
func TestWebhookBot_Builder(t *testing.T) {
	handlerCalled := false
	b := botcommon.NewBuilder().
		WithTenant("acme").
		WithUniverse("cn-prod").
		OnMessage(func(_ botcommon.IncomingMessage) botcommon.OutgoingMessage {
			handlerCalled = true
			return botcommon.OutgoingMessage{}
		})

	bot := New(":9000", b)
	bot.Handler(botcommon.IncomingMessage{Text: "hi"})
	if !handlerCalled {
		t.Error("Handler not invoked")
	}
}

// --- §2.10 Stage 1 增量测试(2026-06-28 youhaoxi)---

// helpers ---

func makeRequest(method, target string, body any, headers map[string]string) *http.Request {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, target, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func decodeWebhookResp(t *testing.T, body io.Reader) webhookResponse {
	t.Helper()
	var resp webhookResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

// --- handleIncoming 测试 ---

func TestHandleIncoming_Success(t *testing.T) {
	b := botcommon.NewBuilder()
	var received botcommon.IncomingMessage
	var called atomic.Int32
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		received = in
		called.Add(1)
		return botcommon.OutgoingMessage{Text: "reply"}
	})
	bot := New(":0", b)
	bot.Handler = b.Handler() // 同步设上

	body := webhookPayload{ChannelID: "ch-1", UserID: "u-1", Text: "hello"}
	req := makeRequest(http.MethodPost, "/v1/bot/webhook/incoming", body, nil)
	rw := httptest.NewRecorder()

	bot.handleIncoming(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rw.Code)
	}
	if called.Load() != 1 {
		t.Errorf("handler called %d times, want 1", called.Load())
	}
	if received.Text != "hello" {
		t.Errorf("Text = %q, want hello", received.Text)
	}
	if received.ChannelID != "ch-1" {
		t.Errorf("ChannelID = %q", received.ChannelID)
	}
}

func TestHandleIncoming_WrongMethod(t *testing.T) {
	bot := New(":0", botcommon.NewBuilder())
	req := httptest.NewRequest(http.MethodGet, "/v1/bot/webhook/incoming", nil)
	rw := httptest.NewRecorder()
	bot.handleIncoming(rw, req)
	if rw.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rw.Code)
	}
}

func TestHandleIncoming_InvalidAPIKey(t *testing.T) {
	b := botcommon.NewBuilder()
	bot := New(":0", b)
	bot.SetAPIKey("correct-key")

	body := webhookPayload{ChannelID: "ch-1", UserID: "u-1", Text: "x"}
	req := makeRequest(http.MethodPost, "/v1/bot/webhook/incoming", body, map[string]string{
		"Authorization": "Bearer wrong-key",
	})
	rw := httptest.NewRecorder()
	bot.handleIncoming(rw, req)
	if rw.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rw.Code)
	}
	resp := decodeWebhookResp(t, rw.Body)
	if resp.OK {
		t.Error("expected OK=false")
	}
	if !strings.Contains(resp.Error, "暂时无法回复") {
		t.Errorf("expected user-facing error, got %q", resp.Error)
	}
}

func TestHandleIncoming_MissingAPIKey(t *testing.T) {
	b := botcommon.NewBuilder()
	bot := New(":0", b)
	bot.SetAPIKey("correct-key")

	body := webhookPayload{ChannelID: "ch-1", UserID: "u-1", Text: "x"}
	req := makeRequest(http.MethodPost, "/v1/bot/webhook/incoming", body, nil) // 无 Authorization
	rw := httptest.NewRecorder()
	bot.handleIncoming(rw, req)
	if rw.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rw.Code)
	}
}

func TestHandleIncoming_NoAPIKeyConfigured_AllowsRequest(t *testing.T) {
	b := botcommon.NewBuilder()
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		return botcommon.OutgoingMessage{Text: "ok"}
	})
	bot := New(":0", b)
	bot.Handler = b.Handler()
	// 不设 APIKey

	body := webhookPayload{ChannelID: "ch-1", UserID: "u-1", Text: "x"}
	req := makeRequest(http.MethodPost, "/v1/bot/webhook/incoming", body, nil)
	rw := httptest.NewRecorder()
	bot.handleIncoming(rw, req)
	if rw.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (no auth required)", rw.Code)
	}
}

func TestHandleIncoming_InvalidJSON(t *testing.T) {
	b := botcommon.NewBuilder()
	bot := New(":0", b)
	req := httptest.NewRequest(http.MethodPost, "/v1/bot/webhook/incoming", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	bot.handleIncoming(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

func TestHandleIncoming_MissingRequiredFields(t *testing.T) {
	b := botcommon.NewBuilder()
	bot := New(":0", b)

	// 缺 channel_id
	body := webhookPayload{UserID: "u-1", Text: "x"}
	req := makeRequest(http.MethodPost, "/v1/bot/webhook/incoming", body, nil)
	rw := httptest.NewRecorder()
	bot.handleIncoming(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("missing channel_id: status = %d, want 400", rw.Code)
	}

	// 缺 user_id
	body2 := webhookPayload{ChannelID: "ch-1", Text: "x"}
	req2 := makeRequest(http.MethodPost, "/v1/bot/webhook/incoming", body2, nil)
	rw2 := httptest.NewRecorder()
	bot.handleIncoming(rw2, req2)
	if rw2.Code != http.StatusBadRequest {
		t.Errorf("missing user_id: status = %d, want 400", rw2.Code)
	}
}

func TestHandleIncoming_NoHandler(t *testing.T) {
	bot := New(":0", botcommon.NewBuilder()) // 无 handler
	body := webhookPayload{ChannelID: "ch-1", UserID: "u-1", Text: "x"}
	req := makeRequest(http.MethodPost, "/v1/bot/webhook/incoming", body, nil)
	rw := httptest.NewRecorder()
	bot.handleIncoming(rw, req)
	if rw.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rw.Code)
	}
}

func TestHandleIncoming_EmptyReply(t *testing.T) {
	b := botcommon.NewBuilder()
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		return botcommon.OutgoingMessage{Text: ""}
	})
	bot := New(":0", b)
	bot.Handler = b.Handler()

	body := webhookPayload{ChannelID: "ch-1", UserID: "u-1", Text: "x"}
	req := makeRequest(http.MethodPost, "/v1/bot/webhook/incoming", body, nil)
	rw := httptest.NewRecorder()
	bot.handleIncoming(rw, req)
	if rw.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rw.Code)
	}
}

// --- SubmitToCore 测试 ---

func TestSubmitToCore_NoClient_ReturnsUserFacing(t *testing.T) {
	bot := New(":0", botcommon.NewBuilder())
	_, err := bot.SubmitToCore(context.Background(), "test")
	if err == nil {
		t.Fatal("SubmitToCore without client should error")
	}
	if !strings.Contains(err.Error(), userFacingErrPrefix) {
		t.Errorf("error = %q, want user-facing", err.Error())
	}
}

// --- Start / Stop 测试 ---

func TestStart_StartsServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	bot := New(ln.Addr().String(), botcommon.NewBuilder())
	if err := bot.StartWithListener(ln); err != nil {
		t.Fatalf("StartWithListener: %v", err)
	}
	defer bot.Stop(context.Background())
}

func TestStart_AlreadyStarted(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	bot := New(ln.Addr().String(), botcommon.NewBuilder())
	if err := bot.StartWithListener(ln); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer bot.Stop(context.Background())

	if err := bot.Start(context.Background()); err == nil {
		t.Fatal("second Start should error")
	}
}

func TestStop_NotStarted_NoError(t *testing.T) {
	bot := New(":0", botcommon.NewBuilder())
	if err := bot.Stop(context.Background()); err != nil {
		t.Errorf("Stop without Start should no-op, got %v", err)
	}
}

func TestStop_Idempotent(t *testing.T) {
	bot := New(":0", botcommon.NewBuilder())
	_ = bot.Start(context.Background())
	_ = bot.Stop(context.Background())
	_ = bot.Stop(context.Background()) // 第二次不 panic
}

// --- 链式 API 测试 ---

func TestWithTenant_Chainable(t *testing.T) {
	bot := New(":0", botcommon.NewBuilder())
	ret := bot.WithTenant("new-tenant")
	if ret != bot {
		t.Error("WithTenant should return same bot")
	}
	if bot.Tenant != "new-tenant" {
		t.Errorf("Tenant = %q", bot.Tenant)
	}
}

func TestWithUniverse_Chainable(t *testing.T) {
	bot := New(":0", botcommon.NewBuilder())
	ret := bot.WithUniverse("new-universe")
	if ret != bot {
		t.Error("WithUniverse should return same bot")
	}
	if bot.Universe != "new-universe" {
		t.Errorf("Universe = %q", bot.Universe)
	}
}

// --- 端到端测试(真实 HTTP server) ---

func TestEndToEnd_HandleIncoming(t *testing.T) {
	// 用真实随机端口启 server(避免 :0 端口冲突)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	b := botcommon.NewBuilder()
	var processed atomic.Int32
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		processed.Add(1)
		return botcommon.OutgoingMessage{Text: "ok"}
	})
	bot := New(addr, b)
	bot.Handler = b.Handler()
	if err := bot.StartWithListener(ln); err != nil {
		t.Fatalf("StartWithListener: %v", err)
	}
	defer bot.Stop(context.Background())

	// 等 server 启动 ready
	time.Sleep(20 * time.Millisecond)

	body := webhookPayload{ChannelID: "ch-1", UserID: "u-1", Text: "hi"}
	resp, err := http.Post("http://"+addr+"/v1/bot/webhook/incoming", "application/json",
		strings.NewReader(mustJSON(t, body)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	// 等 handler 处理
	deadline := time.Now().Add(1 * time.Second)
	for processed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if processed.Load() != 1 {
		t.Errorf("processed = %d, want 1", processed.Load())
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, _ := json.Marshal(v)
	return string(b)
}

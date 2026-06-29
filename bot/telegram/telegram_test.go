package bottelegram

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/wau/wau-go-sdk/bot/common"
)

// TestTelegramBot_New 验证 New(token, builder) 返回非 nil Bot。
func TestTelegramBot_New(t *testing.T) {
	b := botcommon.NewBuilder().
		WithTenant("acme").
		WithUniverse("us-prod").
		OnMessage(func(msg botcommon.IncomingMessage) botcommon.OutgoingMessage {
			return botcommon.OutgoingMessage{Text: "echo: " + msg.Text}
		})

	bot := New("1234:test-token", b)
	if bot == nil {
		t.Fatal("New returned nil")
	}
	if bot.Token != "1234:test-token" {
		t.Errorf("Token = %q, want %q", bot.Token, "1234:test-token")
	}
	if bot.Tenant != "acme" {
		t.Errorf("Tenant = %q, want %q", bot.Tenant, "acme")
	}
	if bot.Universe != "us-prod" {
		t.Errorf("Universe = %q, want %q", bot.Universe, "us-prod")
	}
	if bot.Handler == nil {
		t.Error("Handler should be set from builder")
	}
}

// TestTelegramBot_StartStop Stage 1 实装(2026-06-28):Start 会真连 Telegram API,
//   测试环境若无网络会失败 → 跳过。需要测 Start 用 TestStart_WithMock_NoNetwork。
func TestTelegramBot_StartStop(t *testing.T) {
	t.Skip("Stage 1: Start 真连 Telegram API,无网络会失败。改用 TestStart_WithMock_NoNetwork")
}

// TestTelegramBot_OnMessageChain 验证 OnMessage 链式调用 + handler 覆盖。
func TestTelegramBot_OnMessageChain(t *testing.T) {
	called := false
	bot := New("t", botcommon.NewBuilder()).
		OnMessage(func(_ botcommon.IncomingMessage) botcommon.OutgoingMessage {
			called = true
			return botcommon.OutgoingMessage{Text: "ok"}
		}).
		(*TelegramBot)

	if bot.Handler == nil {
		t.Fatal("Handler not set after OnMessage chain")
	}
	bot.Handler(botcommon.IncomingMessage{Text: "hi"})
	if !called {
		t.Error("Handler not called")
	}
}

// --- §2.8 Stage 1 增量测试(2026-06-28 youhaoxi)---

// mockTelegramAPI mock Telegram Bot API。
type mockTelegramAPI struct {
	mu              sync.Mutex
	updatesCh       chan tgbotapi.Update
	sendCalled      atomic.Int32
	getUpdatesCalls atomic.Int32
	stopCalls       atomic.Int32
	sendErr         error
}

func newMockTelegramAPI() *mockTelegramAPI {
	return &mockTelegramAPI{updatesCh: make(chan tgbotapi.Update, 16)}
}

func (m *mockTelegramAPI) GetUpdatesChan(_ tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	m.getUpdatesCalls.Add(1)
	return m.updatesCh
}

func (m *mockTelegramAPI) StopReceivingUpdates() { m.stopCalls.Add(1) }

func (m *mockTelegramAPI) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	m.sendCalled.Add(1)
	if m.sendErr != nil {
		return tgbotapi.Message{}, m.sendErr
	}
	return tgbotapi.Message{MessageID: 999}, nil
}

func (m *mockTelegramAPI) push(upd tgbotapi.Update) { m.updatesCh <- upd }

// --- NewWithClient 测试 ---

func TestNewWithClient_SetsFields(t *testing.T) {
	b := botcommon.NewBuilder()
	mock := newMockTelegramAPI()
	bot := NewWithClient("token", b, nil, mock)
	if bot.tgAPI == nil {
		t.Error("tgAPI should be set from NewWithClient")
	}
	if bot.tgAPI != mock {
		t.Error("tgAPI should equal mock")
	}
}

// --- 链式 API 测试 ---

func TestWithTenant_Chainable(t *testing.T) {
	b := botcommon.NewBuilder()
	bot := New("token", b)
	ret := bot.WithTenant("new-tenant")
	if ret != bot {
		t.Error("WithTenant should return same bot for chaining")
	}
	if bot.Tenant != "new-tenant" {
		t.Errorf("Tenant = %q, want new-tenant", bot.Tenant)
	}
}

func TestWithUniverse_Chainable(t *testing.T) {
	b := botcommon.NewBuilder()
	bot := New("token", b)
	ret := bot.WithUniverse("new-universe")
	if ret != bot {
		t.Error("WithUniverse should return same bot for chaining")
	}
	if bot.Universe != "new-universe" {
		t.Errorf("Universe = %q, want new-universe", bot.Universe)
	}
}

// --- replyToID 测试 ---

func TestReplyToID_NoReply(t *testing.T) {
	m := &tgbotapi.Message{}
	if got := replyToID(m); got != "" {
		t.Errorf("replyToID = %q, want empty", got)
	}
}

func TestReplyToID_WithReply(t *testing.T) {
	m := &tgbotapi.Message{ReplyToMessage: &tgbotapi.Message{MessageID: 42}}
	if got := replyToID(m); got != "42" {
		t.Errorf("replyToID = %q, want 42", got)
	}
}

// --- processMessage 测试 ---

func TestProcessMessage_HandlerTriggered(t *testing.T) {
	mock := newMockTelegramAPI()
	b := botcommon.NewBuilder()
	var received botcommon.IncomingMessage
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		received = in
		return botcommon.OutgoingMessage{Text: "reply"}
	})
	bot := NewWithClient("token", b, nil, mock)

	bot.processMessage(context.Background(), &tgbotapi.Message{
		MessageID: 100,
		Chat:      &tgbotapi.Chat{ID: 555},
		From:      &tgbotapi.User{ID: 1, UserName: "alice"},
		Text:      "hello",
	})

	if received.Text != "hello" {
		t.Errorf("Text = %q, want hello", received.Text)
	}
	if received.ChannelID != "555" {
		t.Errorf("ChannelID = %q, want 555", received.ChannelID)
	}
	if received.UserID != "1" {
		t.Errorf("UserID = %q, want 1", received.UserID)
	}
	if received.Username != "alice" {
		t.Errorf("Username = %q, want alice", received.Username)
	}
	if received.PlatformMsgID != "100" {
		t.Errorf("PlatformMsgID = %q, want 100", received.PlatformMsgID)
	}
	if mock.sendCalled.Load() != 1 {
		t.Errorf("Send called %d times, want 1", mock.sendCalled.Load())
	}
}

func TestProcessMessage_EmptyReply_NotSent(t *testing.T) {
	mock := newMockTelegramAPI()
	b := botcommon.NewBuilder()
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		return botcommon.OutgoingMessage{Text: ""}
	})
	bot := NewWithClient("token", b, nil, mock)

	bot.processMessage(context.Background(), &tgbotapi.Message{
		MessageID: 1, Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "x",
	})
	if mock.sendCalled.Load() != 0 {
		t.Errorf("Send should not be called for empty reply, got %d", mock.sendCalled.Load())
	}
}

func TestProcessMessage_NoHandler_Dropped(t *testing.T) {
	mock := newMockTelegramAPI()
	b := botcommon.NewBuilder()
	bot := NewWithClient("token", b, nil, mock)

	bot.processMessage(context.Background(), &tgbotapi.Message{
		MessageID: 1, Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "x",
	})
	if mock.sendCalled.Load() != 0 {
		t.Error("Send should not be called when no handler")
	}
}

func TestProcessMessage_SendErrorSwallowed(t *testing.T) {
	mock := newMockTelegramAPI()
	mock.sendErr = errors.New("network fail: should not leak")
	b := botcommon.NewBuilder()
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		return botcommon.OutgoingMessage{Text: "reply"}
	})
	bot := NewWithClient("token", b, nil, mock)

	bot.processMessage(context.Background(), &tgbotapi.Message{
		MessageID: 1, Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "x",
	})
	if mock.sendCalled.Load() != 1 {
		t.Errorf("Send should be called once (then swallowed), got %d", mock.sendCalled.Load())
	}
}

func TestProcessMessage_WithReplyTo(t *testing.T) {
	mock := newMockTelegramAPI()
	b := botcommon.NewBuilder()
	var received botcommon.IncomingMessage
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		received = in
		return botcommon.OutgoingMessage{Text: "ok"}
	})
	bot := NewWithClient("token", b, nil, mock)

	bot.processMessage(context.Background(), &tgbotapi.Message{
		MessageID: 100, Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1},
		Text: "hi", ReplyToMessage: &tgbotapi.Message{MessageID: 50},
	})

	if received.ReplyTo != "50" {
		t.Errorf("ReplyTo = %q, want 50", received.ReplyTo)
	}
}

// --- Start / Stop 测试 ---

func TestStart_WithMock_NoNetwork(t *testing.T) {
	mock := newMockTelegramAPI()
	b := botcommon.NewBuilder()
	bot := NewWithClient("token", b, nil, mock)

	if err := bot.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if mock.getUpdatesCalls.Load() != 1 {
		t.Errorf("GetUpdatesChan called %d times, want 1", mock.getUpdatesCalls.Load())
	}
	defer bot.Stop(context.Background())
}

func TestStart_InvalidToken_ReturnsUserFacing(t *testing.T) {
	b := botcommon.NewBuilder()
	bot := New("", b)
	bot.tgAPI = nil // 强制走 NewBotAPI 路径

	err := bot.Start(context.Background())
	if err == nil {
		t.Fatal("Start with empty token should error")
	}
	mustStr(t, err.Error(), userFacingErrPrefix)
}

func TestStop_CallsStopReceiving(t *testing.T) {
	mock := newMockTelegramAPI()
	b := botcommon.NewBuilder()
	bot := NewWithClient("token", b, nil, mock)
	_ = bot.Start(context.Background())

	if err := bot.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if mock.stopCalls.Load() != 1 {
		t.Errorf("StopReceivingUpdates called %d times, want 1", mock.stopCalls.Load())
	}
}

func TestStop_Idempotent(t *testing.T) {
	mock := newMockTelegramAPI()
	b := botcommon.NewBuilder()
	bot := NewWithClient("token", b, nil, mock)
	_ = bot.Start(context.Background())

	_ = bot.Stop(context.Background())
	_ = bot.Stop(context.Background()) // 第二次不应 panic
}

// --- handleUpdates 端到端 ---

func TestHandleUpdates_ProcessesMessage(t *testing.T) {
	mock := newMockTelegramAPI()
	b := botcommon.NewBuilder()
	var processed atomic.Int32
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		processed.Add(1)
		return botcommon.OutgoingMessage{Text: "ok"}
	})
	bot := NewWithClient("token", b, nil, mock)

	if err := bot.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer bot.Stop(context.Background())

	mock.push(tgbotapi.Update{Message: &tgbotapi.Message{
		MessageID: 1, Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "msg1",
	}})
	mock.push(tgbotapi.Update{Message: &tgbotapi.Message{
		MessageID: 2, Chat: &tgbotapi.Chat{ID: 1}, From: &tgbotapi.User{ID: 1}, Text: "msg2",
	}})

	deadline := time.Now().Add(1 * time.Second)
	for processed.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if processed.Load() != 2 {
		t.Errorf("processed = %d, want 2", processed.Load())
	}
}

func TestHandleUpdates_IgnoresNonMessage(t *testing.T) {
	mock := newMockTelegramAPI()
	b := botcommon.NewBuilder()
	var processed atomic.Int32
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		processed.Add(1)
		return botcommon.OutgoingMessage{Text: "ok"}
	})
	bot := NewWithClient("token", b, nil, mock)

	if err := bot.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer bot.Stop(context.Background())

	mock.push(tgbotapi.Update{Message: nil})

	time.Sleep(50 * time.Millisecond)
	if processed.Load() != 0 {
		t.Errorf("non-message update should be ignored, processed = %d", processed.Load())
	}
}

// --- SubmitToCore 测试 ---

func TestSubmitToCore_NoClient_ReturnsUserFacing(t *testing.T) {
	b := botcommon.NewBuilder()
	bot := New("token", b)
	_, err := bot.SubmitToCore(context.Background(), "test")
	if err == nil {
		t.Fatal("SubmitToCore without client should error")
	}
	mustStr(t, err.Error(), userFacingErrPrefix)
}

// --- helpers ---

func mustStr(t *testing.T, s, substr string) {
	t.Helper()
	if !contains(s, substr) {
		t.Errorf("string %q does not contain %q", s, substr)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

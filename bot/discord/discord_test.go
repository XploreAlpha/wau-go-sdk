package botdiscord

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	discordgo "github.com/bwmarrin/discordgo"

	"github.com/wau/wau-go-sdk/bot/common"
)

// TestDiscordBot_New 验证 New(token, builder)。
func TestDiscordBot_New(t *testing.T) {
	b := botcommon.NewBuilder().
		WithTenant("acme").
		OnMessage(func(msg botcommon.IncomingMessage) botcommon.OutgoingMessage {
			return botcommon.OutgoingMessage{Text: "ack"}
		})

	bot := New("discord-bot-token", b)
	if bot == nil {
		t.Fatal("New returned nil")
	}
	if bot.Token != "discord-bot-token" {
		t.Errorf("Token = %q", bot.Token)
	}
	if bot.Tenant != "acme" {
		t.Errorf("Tenant = %q", bot.Tenant)
	}
}

// TestDiscordBot_StartStop Stage 1 实装:Start 会真连 Discord Gateway,无网络会失败 → 跳过。
// 测 Start 用 TestStart_WithMock_NoNetwork。
func TestDiscordBot_StartStop(t *testing.T) {
	t.Skip("Stage 1: Start 真连 Discord Gateway,无网络会失败。改用 TestStart_WithMock_NoNetwork")
}

// TestDiscordBot_Chain 验证链式调用。
func TestDiscordBot_Chain(t *testing.T) {
	d := New("t", botcommon.NewBuilder().
		WithTenant("t1").
		WithUniverse("us-prod"))
	if d.Tenant != "t1" || d.Universe != "us-prod" {
		t.Errorf("Tenant=%q Universe=%q", d.Tenant, d.Universe)
	}

	// 链式调用 WithTenant / WithUniverse 应返回 botcommon.Bot
	var b botcommon.Bot = d.WithTenant("t2").WithUniverse("cn-prod")
	if b == nil {
		t.Fatal("chain returned nil")
	}
}

// --- §2.9 Stage 1 增量测试(2026-06-28 youhaoxi)---

// mockDiscordAPI mock Discord Bot API。
type mockDiscordAPI struct {
	mu         sync.Mutex
	openCalled atomic.Int32
	closeCalled atomic.Int32
	sendCalled atomic.Int32
	handlerCount atomic.Int32
	openErr   error
	closeErr  error
	sendErr   error
}

func newMockDiscordAPI() *mockDiscordAPI { return &mockDiscordAPI{} }

func (m *mockDiscordAPI) Open() error {
	m.openCalled.Add(1)
	return m.openErr
}

func (m *mockDiscordAPI) Close() error {
	m.closeCalled.Add(1)
	return m.closeErr
}

func (m *mockDiscordAPI) ChannelMessageSend(_, _ string, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	m.sendCalled.Add(1)
	if m.sendErr != nil {
		return nil, m.sendErr
	}
	return &discordgo.Message{ID: "msg-1", Content: "ok"}, nil
}

func (m *mockDiscordAPI) AddHandler(_ interface{}) func() {
	m.handlerCount.Add(1)
	return func() {}
}

// --- NewWithClient 测试 ---

func TestNewWithClient_SetsFields(t *testing.T) {
	b := botcommon.NewBuilder()
	mock := newMockDiscordAPI()
	bot := NewWithClient("token", b, nil, mock)
	if bot.dgAPI == nil {
		t.Error("dgAPI should be set from NewWithClient")
	}
	if bot.botID == "" {
		t.Error("botID should be set in mock mode")
	}
}

// --- stripBotMention 测试 ---

func TestStripBotMention_NoMention(t *testing.T) {
	if got := stripBotMention("hello", "123"); got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestStripBotMention_UserMention(t *testing.T) {
	if got := stripBotMention("<@123> hello", "123"); got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestStripBotMention_NicknameMention(t *testing.T) {
	if got := stripBotMention("<@!123> hi", "123"); got != "hi" {
		t.Errorf("got %q, want hi", got)
	}
}

func TestStripBotMention_EmptyBotID(t *testing.T) {
	if got := stripBotMention("<@123> hello", ""); got != "<@123> hello" {
		t.Errorf("empty botID should not strip, got %q", got)
	}
}

func TestStripBotMention_DifferentID(t *testing.T) {
	if got := stripBotMention("<@999> hello", "123"); got != "<@999> hello" {
		t.Errorf("different ID should not strip, got %q", got)
	}
}

// --- replyToID 测试 ---

func TestReplyToID_NoReferenced(t *testing.T) {
	m := &discordgo.Message{}
	if got := replyToID(m); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestReplyToID_WithReferenced(t *testing.T) {
	m := &discordgo.Message{ReferencedMessage: &discordgo.Message{ID: "msg-50"}}
	if got := replyToID(m); got != "msg-50" {
		t.Errorf("got %q, want msg-50", got)
	}
}

// --- onMessageCreate 测试 ---

func TestOnMessageCreate_HandlerTriggered(t *testing.T) {
	mock := newMockDiscordAPI()
	b := botcommon.NewBuilder()
	var received botcommon.IncomingMessage
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		received = in
		return botcommon.OutgoingMessage{Text: "reply"}
	})
	bot := NewWithClient("token", b, nil, mock)
	bot.botID = "self-bot-id"

	bot.onMessageCreate(nil, &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "msg-100",
			ChannelID: "ch-555",
			Content:   "<@!self-bot-id> hello",
			Author:    &discordgo.User{ID: "user-1", Username: "alice"},
		},
	})

	if received.Text != "hello" {
		t.Errorf("Text = %q, want hello (after @bot strip)", received.Text)
	}
	if received.ChannelID != "ch-555" {
		t.Errorf("ChannelID = %q, want ch-555", received.ChannelID)
	}
	if received.UserID != "user-1" {
		t.Errorf("UserID = %q, want user-1", received.UserID)
	}
	if received.Username != "alice" {
		t.Errorf("Username = %q, want alice", received.Username)
	}
	if received.PlatformMsgID != "msg-100" {
		t.Errorf("PlatformMsgID = %q, want msg-100", received.PlatformMsgID)
	}
	if mock.sendCalled.Load() != 1 {
		t.Errorf("Send called %d times, want 1", mock.sendCalled.Load())
	}
}

func TestOnMessageCreate_IgnoreBotSelf(t *testing.T) {
	mock := newMockDiscordAPI()
	b := botcommon.NewBuilder()
	var processed atomic.Int32
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		processed.Add(1)
		return botcommon.OutgoingMessage{Text: "ok"}
	})
	bot := NewWithClient("token", b, nil, mock)
	bot.botID = "self-bot-id"

	bot.onMessageCreate(nil, &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:     "msg-1",
			Author: &discordgo.User{ID: "self-bot-id", Username: "bot"}, // == botID
		},
	})
	if processed.Load() != 0 {
		t.Error("bot self message should be ignored")
	}
	if mock.sendCalled.Load() != 0 {
		t.Error("Send should not be called for self message")
	}
}

func TestOnMessageCreate_EmptyReply_NotSent(t *testing.T) {
	mock := newMockDiscordAPI()
	b := botcommon.NewBuilder()
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		return botcommon.OutgoingMessage{Text: ""}
	})
	bot := NewWithClient("token", b, nil, mock)

	bot.onMessageCreate(nil, &discordgo.MessageCreate{
		Message: &discordgo.Message{Author: &discordgo.User{ID: "u"}},
	})
	if mock.sendCalled.Load() != 0 {
		t.Error("empty reply should not be sent")
	}
}

func TestOnMessageCreate_NoHandler_Dropped(t *testing.T) {
	mock := newMockDiscordAPI()
	b := botcommon.NewBuilder()
	bot := NewWithClient("token", b, nil, mock)

	bot.onMessageCreate(nil, &discordgo.MessageCreate{
		Message: &discordgo.Message{Author: &discordgo.User{ID: "u"}},
	})
	if mock.sendCalled.Load() != 0 {
		t.Error("no handler should drop message")
	}
}

func TestOnMessageCreate_SendErrorSwallowed(t *testing.T) {
	mock := newMockDiscordAPI()
	mock.sendErr = errors.New("discord rate limit: should not leak")
	b := botcommon.NewBuilder()
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		return botcommon.OutgoingMessage{Text: "reply"}
	})
	bot := NewWithClient("token", b, nil, mock)

	// 0 门槛 UX:Send 失败不外泄
	bot.onMessageCreate(nil, &discordgo.MessageCreate{
		Message: &discordgo.Message{ChannelID: "ch-1", Author: &discordgo.User{ID: "u"}},
	})
	if mock.sendCalled.Load() != 1 {
		t.Error("Send should be called once (then swallowed)")
	}
}

func TestOnMessageCreate_NilMessage(t *testing.T) {
	mock := newMockDiscordAPI()
	b := botcommon.NewBuilder()
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		return botcommon.OutgoingMessage{Text: "ok"}
	})
	bot := NewWithClient("token", b, nil, mock)
	bot.onMessageCreate(nil, nil) // 不应 panic
	if mock.sendCalled.Load() != 0 {
		t.Error("nil message should not trigger")
	}
}

func TestOnMessageCreate_WithReplyTo(t *testing.T) {
	mock := newMockDiscordAPI()
	b := botcommon.NewBuilder()
	var received botcommon.IncomingMessage
	b.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
		received = in
		return botcommon.OutgoingMessage{Text: "ok"}
	})
	bot := NewWithClient("token", b, nil, mock)

	bot.onMessageCreate(nil, &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ChannelID:        "ch-1",
			Author:           &discordgo.User{ID: "u"},
			ReferencedMessage: &discordgo.Message{ID: "msg-50"},
		},
	})
	if received.ReplyTo != "msg-50" {
		t.Errorf("ReplyTo = %q, want msg-50", received.ReplyTo)
	}
}

// --- Start / Stop 测试 ---

func TestStart_WithMock_NoNetwork(t *testing.T) {
	mock := newMockDiscordAPI()
	b := botcommon.NewBuilder()
	bot := NewWithClient("token", b, nil, mock)

	if err := bot.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if mock.openCalled.Load() != 1 {
		t.Errorf("Open called %d times, want 1", mock.openCalled.Load())
	}
	if mock.handlerCount.Load() != 1 {
		t.Errorf("AddHandler called %d times, want 1", mock.handlerCount.Load())
	}
	defer bot.Stop(context.Background())
}

func TestStart_OpenError_ReturnsUserFacing(t *testing.T) {
	mock := newMockDiscordAPI()
	mock.openErr = errors.New("network fail: should not leak")
	b := botcommon.NewBuilder()
	bot := NewWithClient("token", b, nil, mock)

	err := bot.Start(context.Background())
	if err == nil {
		t.Fatal("Start with open error should error")
	}
	if !contains(err.Error(), userFacingErrPrefix) {
		t.Errorf("error = %q, want user-facing", err.Error())
	}
}

func TestStop_CallsClose(t *testing.T) {
	mock := newMockDiscordAPI()
	b := botcommon.NewBuilder()
	bot := NewWithClient("token", b, nil, mock)
	_ = bot.Start(context.Background())

	if err := bot.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if mock.closeCalled.Load() != 1 {
		t.Errorf("Close called %d times, want 1", mock.closeCalled.Load())
	}
}

func TestStop_Idempotent(t *testing.T) {
	mock := newMockDiscordAPI()
	b := botcommon.NewBuilder()
	bot := NewWithClient("token", b, nil, mock)
	_ = bot.Start(context.Background())

	_ = bot.Stop(context.Background())
	_ = bot.Stop(context.Background()) // 第二次不应 panic
}

// --- SubmitToCore 测试 ---

func TestSubmitToCore_NoClient_ReturnsUserFacing(t *testing.T) {
	b := botcommon.NewBuilder()
	bot := New("token", b)
	_, err := bot.SubmitToCore(context.Background(), "test")
	if err == nil {
		t.Fatal("SubmitToCore without client should error")
	}
	if !contains(err.Error(), userFacingErrPrefix) {
		t.Errorf("error = %q, want user-facing", err.Error())
	}
}

// --- 链式 API 测试 ---

func TestWithTenant_Chainable(t *testing.T) {
	b := botcommon.NewBuilder()
	bot := New("token", b)
	ret := bot.WithTenant("new-tenant")
	if ret != bot {
		t.Error("WithTenant should return same bot")
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
		t.Error("WithUniverse should return same bot")
	}
	if bot.Universe != "new-universe" {
		t.Errorf("Universe = %q", bot.Universe)
	}
}

// --- helpers ---

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

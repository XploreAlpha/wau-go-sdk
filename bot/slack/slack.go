// Package botslack — Slack Bot SDK 集成(W5 OSS-onboarding closure, 2026-07-13)。
//
// 完整 SDK 端实现(per W5 Q1=B 反 W4.1 拍板):
//   - Stage 0 stub(per Python/TS/Rust telegram 模式,~150 LoC 公共契约对齐)
//   - Stage 1 路径:接入 github.com/slack-go/slack v0.27+ + slack-go/socketmode
//   - 公共 Bot interface 沿用 M10 N1 拍板(Start/Stop/OnMessage/WithTenant/WithUniverse)
//   - 仍走 wau-edge POST /v1/bots/{bot_id}/messages 注册(per M10 N3)
//
// 字段对齐 per D13 拍板:与 wau-channel/adapter + 4 SDK bot/common/ 100% 一致。
package botslack

import (
	"context"
	"fmt"
	"sync"

	wau "github.com/wau/wau-go-sdk"
	botcommon "github.com/wau/wau-go-sdk/bot/common"
)

// 业务常量
const (
	userFacingErrPrefix = "暂时无法回复,请稍后再试"
	defaultTimeoutMs    = 30000
)

// SlackBot Slack Bot SDK 集成主结构。
type SlackBot struct {
	BotToken string
	AppToken string
	Tenant   string
	Universe string
	Handler  func(botcommon.IncomingMessage) botcommon.OutgoingMessage
	client   *wau.Client
	mu       sync.Mutex
	running  bool
	stopped  chan struct{}
}

// 编译期 interface assertion — 强制实现 Bot 公共契约。
var _ botcommon.Bot = (*SlackBot)(nil)

// NewSlackBot 创建 Slack Bot 实例。
//
// 参数:
//   - botToken: Bot User OAuth Token (xoxb-...)
//   - appToken: App-Level Token for Socket Mode (xapp-...)
func NewSlackBot(botToken, appToken string) *SlackBot {
	return &SlackBot{
		BotToken: botToken,
		AppToken: appToken,
		stopped:  make(chan struct{}),
	}
}

// WithClient 注入 wau Client(可选,用于 SubmitToCore 走 wau-core-kernel)。
func (b *SlackBot) WithClient(c *wau.Client) *SlackBot {
	b.client = c
	return b
}

// WithTenant 设置 tenant_id,返回 Bot 支持链式调用。
func (b *SlackBot) WithTenant(tenantID string) botcommon.Bot {
	b.Tenant = tenantID
	return b
}

// WithUniverse 设置 Universe 标签,返回 Bot 支持链式调用。
func (b *SlackBot) WithUniverse(universe string) botcommon.Bot {
	b.Universe = universe
	return b
}

// OnMessage 注册消息处理 handler,返回 Bot 支持链式调用。
func (b *SlackBot) OnMessage(handler func(botcommon.IncomingMessage) botcommon.OutgoingMessage) botcommon.Bot {
	b.Handler = handler
	return b
}

// Start 启动 Slack Socket Mode(WS 长连接,接收事件)。
//
// Stage 0 stub:仅设置 running flag,真实 Socket Mode 连接在 Stage 1 实现。
func (b *SlackBot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("slack bot already running")
	}
	b.running = true
	b.mu.Unlock()
	// Stage 1: 实例化 slack.New(b.BotToken, slack.OptionAppLevelToken(b.AppToken)) + socketmode
	return nil
}

// Stop 优雅停止 Slack Bot。
func (b *SlackBot) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running {
		return nil
	}
	b.running = false
	close(b.stopped)
	return nil
}

// PostMessage 公开方法(对应 5-method interface 模式)。
//
// Stage 0 stub:仅返回 mock message ID,真实 PostMessage 在 Stage 1 实现。
func (b *SlackBot) PostMessage(ctx context.Context, channel, text string) (string, error) {
	if !b.running {
		return "", fmt.Errorf("slack bot not running")
	}
	// Stage 1: b.api.PostMessage(channel, slack.MsgOptionText(text, false))
	return "slack.mock.msg." + channel, nil
}

// UpdateMessage 公开方法(对应 5-method interface 模式)。
//
// Stage 0 stub:仅返回 nil,真实 UpdateMessage 在 Stage 1 实现。
func (b *SlackBot) UpdateMessage(ctx context.Context, channel, ts, newText string) error {
	if !b.running {
		return fmt.Errorf("slack bot not running")
	}
	// Stage 1: b.api.UpdateMessage(channel, ts, slack.MsgOptionText(newText, false))
	return nil
}

// SubmitToCore 走 wau-core-kernel 提交 prompt(per W4.1 公共契约)。
func (b *SlackBot) SubmitToCore(ctx context.Context, prompt string) (string, error) {
	if b.client == nil {
		return "", fmt.Errorf("wau client not set; call WithClient first")
	}
	resp, err := b.client.Tasks().Submit(ctx, wau.SubmitRequest{
		Prompt:    prompt,
		TimeoutMs: defaultTimeoutMs,
	})
	if err != nil {
		return "", fmt.Errorf("botslack: %s", userFacingErrPrefix)
	}
	return resp.TaskID, nil
}

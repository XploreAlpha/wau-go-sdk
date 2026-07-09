// Package botslack — Slack Bot SDK 集成(W6.2 Stage 1 native SDK integration, 2026-07-09)。
//
// Stage 1 native 实现(替换 W5 Stage 0 stub):
//   - 长连接:github.com/slack-go/slack + slack-go/socketmode(Socket Mode WS)
//   - 发送 / 编辑:slack.Client.PostMessage(chat.postMessage) / UpdateMessage(chat.update)
//   - 事件流:socketmode.EventTypeEventsAPI → slackevents.MessageEvent → IncomingMessage
//   - 公共 Bot interface 沿用 M10 N1 拍板(Start/Stop/OnMessage/WithTenant/WithUniverse)
//   - SubmitToCore 走 wau-core-kernel wau.Client.Tasks().Submit(per W7 模板)
//
// 协议合规(per wau-channel/adapter/slack/slack_real.go W7 2026-07-07 SDK 接通):
//   - D60 additive:老 bot/ 子包(telegram/discord/webhook)0 改动
//   - 公共 Bot interface 5 方法签名一字不改
//
// 字段对齐 per D13 拍板:与 wau-channel/adapter + 4 SDK bot/common/ 100% 一致。
package botslack

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	wau "github.com/wau/wau-go-sdk"
	botcommon "github.com/wau/wau-go-sdk/bot/common"
)

// 业务常量
const (
	userFacingErrPrefix = "暂时无法回复,请稍后再试"
	defaultTimeoutMs    = 30000
)

// SlackBot Slack Bot SDK 集成主结构(Stage 1 native)。
type SlackBot struct {
	BotToken string
	AppToken string
	Tenant   string
	Universe string
	Handler  func(botcommon.IncomingMessage) botcommon.OutgoingMessage
	client   *wau.Client

	// native SDK 句柄(Start 时构造)
	api   *slack.Client
	sm    *socketmode.Client
	botID string

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	stopped chan struct{}
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
// 步骤:
//  1. 校验 token(0 门槛 UX:fail-fast)
//  2. slack.New + slack.OptionAppLevelToken
//  3. AuthTest 拿 botID(D80 透传,自身消息过滤用)
//  4. socketmode.New(api) + 后台 goroutine RunContext + 事件循环
func (b *SlackBot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("slack bot already running")
	}
	if b.BotToken == "" || b.AppToken == "" {
		b.mu.Unlock()
		return fmt.Errorf("botslack: bot/app token required (Socket Mode needs xoxb- + xapp-)")
	}

	b.api = slack.New(b.BotToken, slack.OptionAppLevelToken(b.AppToken))
	auth, err := b.api.AuthTestContext(ctx)
	if err != nil {
		b.mu.Unlock()
		return fmt.Errorf("botslack: auth.test failed: %w", err)
	}
	b.botID = auth.UserID
	b.sm = socketmode.New(b.api)

	runCtx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	b.running = true
	b.mu.Unlock()

	go func() { _ = b.sm.RunContext(runCtx) }()
	go b.eventLoop(runCtx)
	return nil
}

// eventLoop 消费 socketmode 事件流,过滤 message 事件后 dispatch。
func (b *SlackBot) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-b.sm.Events:
			if !ok {
				return
			}
			if evt.Type != socketmode.EventTypeEventsAPI {
				continue
			}
			apiEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
			if !ok {
				continue
			}
			if evt.Request != nil {
				_ = b.sm.Ack(*evt.Request)
			}
			if apiEvent.Type != slackevents.CallbackEvent {
				continue
			}
			msgEvt, ok := apiEvent.InnerEvent.Data.(*slackevents.MessageEvent)
			if !ok || msgEvt == nil {
				continue
			}
			// 过滤自身 bot 消息 + 子类型事件(edited/deleted 等)
			if msgEvt.User == "" || msgEvt.User == b.botID || msgEvt.BotID != "" {
				continue
			}
			b.dispatch(ctx, msgEvt)
		}
	}
}

// dispatch 归一化为 IncomingMessage → Handler → PostMessage 回复。
func (b *SlackBot) dispatch(ctx context.Context, e *slackevents.MessageEvent) {
	if b.Handler == nil {
		return
	}
	in := botcommon.IncomingMessage{
		PlatformMsgID: e.TimeStamp,
		ChannelID:     e.Channel,
		UserID:        e.User,
		Username:      e.Username,
		Text:          e.Text,
		ReplyTo:       e.ThreadTimeStamp,
		Timestamp:     time.Now(),
	}
	out := b.Handler(in)
	if out.Text == "" {
		return
	}
	if _, err := b.PostMessage(ctx, e.Channel, out.Text); err != nil {
		return
	}
}

// Stop 优雅停止 Slack Bot。
func (b *SlackBot) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running {
		return nil
	}
	b.running = false
	if b.cancel != nil {
		b.cancel()
	}
	close(b.stopped)
	return nil
}

// PostMessage 调 slack-go/slack chat.postMessage,返回 message TS。
func (b *SlackBot) PostMessage(ctx context.Context, channel, text string) (string, error) {
	if !b.running || b.api == nil {
		return "", fmt.Errorf("slack bot not running")
	}
	_, ts, err := b.api.PostMessageContext(ctx, channel, slack.MsgOptionText(text, false))
	if err != nil {
		return "", fmt.Errorf("botslack: %s", userFacingErrPrefix)
	}
	return ts, nil
}

// UpdateMessage 调 slack-go/slack chat.update(streaming edit 用)。
func (b *SlackBot) UpdateMessage(ctx context.Context, channel, ts, newText string) error {
	if !b.running || b.api == nil {
		return fmt.Errorf("slack bot not running")
	}
	if _, _, _, err := b.api.UpdateMessageContext(ctx, channel, ts, slack.MsgOptionText(newText, false)); err != nil {
		return fmt.Errorf("botslack: %s", userFacingErrPrefix)
	}
	return nil
}

// SubmitToCore 走 wau-core-kernel 提交 prompt(per W7 模板)。
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

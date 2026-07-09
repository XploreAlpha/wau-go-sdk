// Package botdingtalk — DingTalk Bot SDK 集成(W6.2 Stage 1 native SDK integration, 2026-07-09)。
//
// Stage 1 native 实现(替换 W5 Stage 0 stub):
//   - 长连接:github.com/open-dingtalk/dingtalk-stream-sdk-go(StreamClient + chatbot callback)
//   - 回复:每条 incoming 带 sessionWebhook,PostMessage/UpdateMessage 用缓存 webhook reply
//   - 公共 Bot interface 沿用 M10 N1 拍板(Start/Stop/OnMessage/WithTenant/WithUniverse)
//   - SubmitToCore 走 wau-core-kernel wau.Client.Tasks().Submit(per W7 模板)
//
// DingTalk bot 模型说明(Stream Mode):
//   - 钉钉 Stream Mode chatbot 无 "update message" API,只有 reply-by-webhook
//   - UpdateMessage 语义 = reply with new text(保留 messageID 作 caller 关联键)
//   - conversationID 必须先收到 incoming 事件缓存 sessionWebhook 后才能 PostMessage
//
// 协议合规(per wau-channel/adapter/dingtalk/dingtalk_real.go W7 2026-07-07 SDK 接通):
//   - D60 additive:老 bot/ 子包(telegram/discord/webhook)0 改动
//   - 公共 Bot interface 5 方法签名一字不改
package botdingtalk

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	dingtalkstream "github.com/open-dingtalk/dingtalk-stream-sdk-go/client"

	wau "github.com/wau/wau-go-sdk"
	botcommon "github.com/wau/wau-go-sdk/bot/common"
)

const (
	userFacingErrPrefix = "暂时无法回复,请稍后再试"
	defaultTimeoutMs    = 30000
)

// DingTalkBot DingTalk Bot SDK 集成主结构(Stage 1 native)。
type DingTalkBot struct {
	AppKey    string
	AppSecret string
	RobotCode string
	Tenant    string
	Universe  string
	Handler   func(botcommon.IncomingMessage) botcommon.OutgoingMessage
	client    *wau.Client

	// native SDK 句柄(Start 时构造)
	cli      *dingtalkstream.StreamClient
	webhooks map[string]string // conversationID → sessionWebhook URL

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	stopped chan struct{}
}

var _ botcommon.Bot = (*DingTalkBot)(nil)

// NewDingTalkBot 创建 DingTalk Bot 实例。
func NewDingTalkBot(appKey, appSecret, robotCode string) *DingTalkBot {
	return &DingTalkBot{
		AppKey:    appKey,
		AppSecret: appSecret,
		RobotCode: robotCode,
		webhooks:  make(map[string]string),
		stopped:   make(chan struct{}),
	}
}

// WithClient 注入 wau Client。
func (b *DingTalkBot) WithClient(c *wau.Client) *DingTalkBot {
	b.client = c
	return b
}

// WithTenant 设置 tenant_id。
func (b *DingTalkBot) WithTenant(tenantID string) botcommon.Bot {
	b.Tenant = tenantID
	return b
}

// WithUniverse 设置 Universe 标签。
func (b *DingTalkBot) WithUniverse(universe string) botcommon.Bot {
	b.Universe = universe
	return b
}

// OnMessage 注册消息处理 handler。
func (b *DingTalkBot) OnMessage(handler func(botcommon.IncomingMessage) botcommon.OutgoingMessage) botcommon.Bot {
	b.Handler = handler
	return b
}

// Start 启动 DingTalk Stream Mode(WS 长连接 + chatbot callback 路由)。
//
// 步骤:
//  1. 校验 appKey/appSecret(0 门槛 UX)
//  2. StreamClient + WithAppCredential
//  3. RegisterChatBotCallbackRouter(onChatBotMessage)
//  4. cli.Start(ctx)(SDK 内部建连,失败返 err)
func (b *DingTalkBot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("dingtalk bot already running")
	}
	if b.AppKey == "" || b.AppSecret == "" {
		b.mu.Unlock()
		return fmt.Errorf("botdingtalk: app_key/app_secret required")
	}
	runCtx, cancel := context.WithCancel(context.Background())
	cli := dingtalkstream.NewStreamClient(
		dingtalkstream.WithAppCredential(dingtalkstream.NewAppCredentialConfig(b.AppKey, b.AppSecret)),
	)
	cli.RegisterChatBotCallbackRouter(b.onChatBotMessage)
	b.cli = cli
	b.cancel = cancel
	b.running = true
	b.mu.Unlock()

	if err := cli.Start(runCtx); err != nil {
		b.mu.Lock()
		b.running = false
		b.cli = nil
		b.mu.Unlock()
		cancel()
		log.Printf("[botdingtalk] stream start error: %v", err)
		return fmt.Errorf("botdingtalk: %s", userFacingErrPrefix)
	}
	return nil
}

// onChatBotMessage SDK chatbot callback 路由 → IncomingMessage → Handler → reply。
func (b *DingTalkBot) onChatBotMessage(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	if data == nil {
		return []byte(""), nil
	}
	b.cacheWebhook(data.ConversationId, data.SessionWebhook)
	if b.Handler == nil {
		return []byte(""), nil
	}
	in := botcommon.IncomingMessage{
		PlatformMsgID: data.MsgId,
		ChannelID:     data.ConversationId,
		UserID:        data.SenderStaffId,
		Username:      data.SenderNick,
		Text:          data.Text.Content,
		Timestamp:     time.Now(),
	}
	out := b.Handler(in)
	if out.Text == "" {
		return []byte(""), nil
	}
	if _, err := b.PostMessage(ctx, data.ConversationId, out.Text); err != nil {
		return []byte(""), nil
	}
	return []byte(""), nil
}

// Stop 优雅停止。
func (b *DingTalkBot) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running {
		return nil
	}
	b.running = false
	if b.cli != nil {
		b.cli.Close()
	}
	if b.cancel != nil {
		b.cancel()
	}
	close(b.stopped)
	return nil
}

// PostMessage 用缓存 sessionWebhook reply 文本(DingTalk 无独立发消息 API)。
func (b *DingTalkBot) PostMessage(ctx context.Context, conversationID, text string) (string, error) {
	if !b.running {
		return "", fmt.Errorf("dingtalk bot not running")
	}
	webhook := b.lookupWebhook(conversationID)
	if webhook == "" {
		return "", fmt.Errorf("botdingtalk: no sessionWebhook cached for conversation=%q (need incoming message first)", conversationID)
	}
	replier := chatbot.NewChatbotReplier()
	if err := replier.SimpleReplyText(ctx, webhook, []byte(text)); err != nil {
		log.Printf("[botdingtalk] post error (conversation=%s): %v", conversationID, err)
		return "", fmt.Errorf("botdingtalk: %s", userFacingErrPrefix)
	}
	return "dingtalk.reply." + conversationID, nil
}

// UpdateMessage DingTalk 无真 update,语义为 reply with new text(保留 msgID 作关联键)。
func (b *DingTalkBot) UpdateMessage(ctx context.Context, conversationID, msgID, newText string) error {
	if !b.running {
		return fmt.Errorf("dingtalk bot not running")
	}
	webhook := b.lookupWebhook(conversationID)
	if webhook == "" {
		return fmt.Errorf("botdingtalk: no sessionWebhook cached for conversation=%q", conversationID)
	}
	replier := chatbot.NewChatbotReplier()
	if err := replier.SimpleReplyText(ctx, webhook, []byte(newText)); err != nil {
		log.Printf("[botdingtalk] update error (conversation=%s, msg=%s): %v", conversationID, msgID, err)
		return fmt.Errorf("botdingtalk: %s", userFacingErrPrefix)
	}
	return nil
}

// SubmitToCore 走 wau-core-kernel 提交 prompt(per W7 模板)。
func (b *DingTalkBot) SubmitToCore(ctx context.Context, prompt string) (string, error) {
	if b.client == nil {
		return "", fmt.Errorf("wau client not set; call WithClient first")
	}
	resp, err := b.client.Tasks().Submit(ctx, wau.SubmitRequest{
		Prompt:    prompt,
		TimeoutMs: defaultTimeoutMs,
	})
	if err != nil {
		return "", fmt.Errorf("botdingtalk: %s", userFacingErrPrefix)
	}
	return resp.TaskID, nil
}

// --- 内部 helpers ---

// lookupWebhook 读 webhooks 缓存(并发安全)。
func (b *DingTalkBot) lookupWebhook(conversationID string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.webhooks[conversationID]
}

// cacheWebhook 写 webhooks 缓存(由 onChatBotMessage 调)。
func (b *DingTalkBot) cacheWebhook(conversationID, sessionWebhook string) {
	if conversationID == "" || sessionWebhook == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.webhooks[conversationID] = sessionWebhook
}

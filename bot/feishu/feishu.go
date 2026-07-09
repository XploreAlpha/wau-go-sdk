// Package botfeishu — Feishu/Lark Bot SDK 集成(W6.2 Stage 1 native SDK integration, 2026-07-09)。
//
// Stage 1 native 实现(替换 W5 Stage 0 stub):
//   - 长连接:github.com/larksuite/oapi-sdk-go/v3(larkws.Client + EventDispatcher)
//     OnP2MessageReceiveV1 接收 im.message.receive_v1
//   - 发送:larkim.NewCreateMessageReqBuilder + ReceiveIdType("chat_id")
//   - 编辑:larkim.NewPatchMessageReqBuilder + MessageId(...)
//   - 公共 Bot interface 沿用 M10 N1 拍板(Start/Stop/OnMessage/WithTenant/WithUniverse)
//   - SubmitToCore 走 wau-core-kernel wau.Client.Tasks().Submit(per W7 模板)
//
// 协议合规(per wau-channel/adapter/feishu/feishu_real.go W7 2026-07-07 SDK 接通):
//   - D60 additive:老 bot/ 子包(telegram/discord/webhook)0 改动
//   - 公共 Bot interface 5 方法签名一字不改
package botfeishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	wau "github.com/wau/wau-go-sdk"
	botcommon "github.com/wau/wau-go-sdk/bot/common"
)

// 业务常量
const (
	userFacingErrPrefix = "暂时无法回复,请稍后再试"
	defaultTimeoutMs    = 30000
	feishuMsgTypeText   = "text"
	feishuReceiveChat   = "chat_id"
)

// FeishuBot Feishu/Lark Bot SDK 集成主结构(Stage 1 native)。
type FeishuBot struct {
	AppID     string
	AppSecret string
	Tenant    string
	Universe  string
	Handler   func(botcommon.IncomingMessage) botcommon.OutgoingMessage
	client    *wau.Client

	// native SDK 句柄(Start 时构造)
	lark *lark.Client
	ws   *larkws.Client

	mu      sync.Mutex
	running bool
	stopped chan struct{}
}

// 编译期 interface assertion。
var _ botcommon.Bot = (*FeishuBot)(nil)

// NewFeishuBot 创建 Feishu/Lark Bot 实例。
func NewFeishuBot(appID, appSecret string) *FeishuBot {
	return &FeishuBot{
		AppID:     appID,
		AppSecret: appSecret,
		stopped:   make(chan struct{}),
	}
}

// WithClient 注入 wau Client(可选)。
func (b *FeishuBot) WithClient(c *wau.Client) *FeishuBot {
	b.client = c
	return b
}

// WithTenant 设置 tenant_id。
func (b *FeishuBot) WithTenant(tenantID string) botcommon.Bot {
	b.Tenant = tenantID
	return b
}

// WithUniverse 设置 Universe 标签。
func (b *FeishuBot) WithUniverse(universe string) botcommon.Bot {
	b.Universe = universe
	return b
}

// OnMessage 注册消息处理 handler。
func (b *FeishuBot) OnMessage(handler func(botcommon.IncomingMessage) botcommon.OutgoingMessage) botcommon.Bot {
	b.Handler = handler
	return b
}

// Start 启动 Feishu WS 长连接 + 装配 EventDispatcher。
//
// 步骤:
//  1. 校验 appID/appSecret(0 门槛 UX)
//  2. lark.NewClient(REST,用于 PostMessage/UpdateMessage)
//  3. EventDispatcher.OnP2MessageReceiveV1 注册 handler
//  4. larkws.NewClient + Start(阻塞建连,失败返 err)
func (b *FeishuBot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("feishu bot already running")
	}
	if b.AppID == "" || b.AppSecret == "" {
		b.mu.Unlock()
		return fmt.Errorf("botfeishu: app_id/app_secret required")
	}
	b.lark = lark.NewClient(b.AppID, b.AppSecret, lark.WithEnableTokenCache(true))
	b.running = true
	b.mu.Unlock()

	dispatch := larkdispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(c context.Context, ev *larkim.P2MessageReceiveV1) error {
			b.dispatch(c, ev)
			return nil
		})

	ws := larkws.NewClient(b.AppID, b.AppSecret, larkws.WithEventHandler(dispatch))
	b.mu.Lock()
	b.ws = ws
	b.mu.Unlock()

	startCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := ws.Start(startCtx); err != nil {
		b.mu.Lock()
		b.running = false
		b.ws = nil
		b.mu.Unlock()
		return fmt.Errorf("botfeishu: WS start failed: %w", err)
	}
	return nil
}

// dispatch 归一化 larkim 事件 → IncomingMessage → Handler → PostMessage 回复。
func (b *FeishuBot) dispatch(ctx context.Context, ev *larkim.P2MessageReceiveV1) {
	if b.Handler == nil || ev == nil || ev.Event == nil || ev.Event.Message == nil {
		return
	}
	msg := ev.Event.Message
	in := botcommon.IncomingMessage{
		Timestamp: time.Now(),
	}
	if msg.MessageId != nil {
		in.PlatformMsgID = *msg.MessageId
	}
	if msg.ChatId != nil {
		in.ChannelID = *msg.ChatId
	}
	if msg.RootId != nil {
		in.ReplyTo = *msg.RootId
	}
	if msg.Content != nil {
		in.Text = extractTextContent(*msg.Content)
	}
	if ev.Event.Sender != nil && ev.Event.Sender.SenderId != nil {
		if ev.Event.Sender.SenderId.OpenId != nil {
			in.UserID = *ev.Event.Sender.SenderId.OpenId
		}
	}
	out := b.Handler(in)
	if out.Text == "" || in.ChannelID == "" {
		return
	}
	if _, err := b.PostMessage(ctx, in.ChannelID, out.Text); err != nil {
		return
	}
}

// Stop 优雅停止。
func (b *FeishuBot) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running {
		return nil
	}
	b.running = false
	if b.ws != nil {
		b.ws.Close()
	}
	close(b.stopped)
	return nil
}

// PostMessage REST POST /open-apis/im/v1/messages(receive_id_type=chat_id)。
func (b *FeishuBot) PostMessage(ctx context.Context, chatID, text string) (string, error) {
	if !b.running || b.lark == nil {
		return "", fmt.Errorf("feishu bot not running")
	}
	body := larkim.NewCreateMessageReqBodyBuilder().
		ReceiveId(chatID).
		MsgType(feishuMsgTypeText).
		Content(buildTextContent(text)).
		Build()
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(feishuReceiveChat).
		Body(body).
		Build()
	resp, err := b.lark.Im.Message.Create(ctx, req)
	if err != nil || !resp.Success() {
		return "", fmt.Errorf("botfeishu: %s", userFacingErrPrefix)
	}
	if resp.Data != nil && resp.Data.MessageId != nil {
		return *resp.Data.MessageId, nil
	}
	return "", nil
}

// UpdateMessage REST PATCH /open-apis/im/v1/messages/{message_id}。
func (b *FeishuBot) UpdateMessage(ctx context.Context, chatID, msgID, newText string) error {
	if !b.running || b.lark == nil {
		return fmt.Errorf("feishu bot not running")
	}
	body := larkim.NewPatchMessageReqBodyBuilder().
		Content(buildTextContent(newText)).
		Build()
	req := larkim.NewPatchMessageReqBuilder().
		MessageId(msgID).
		Body(body).
		Build()
	resp, err := b.lark.Im.Message.Patch(ctx, req)
	if err != nil || !resp.Success() {
		return fmt.Errorf("botfeishu: %s", userFacingErrPrefix)
	}
	return nil
}

// SubmitToCore 走 wau-core-kernel 提交 prompt(per W7 模板)。
func (b *FeishuBot) SubmitToCore(ctx context.Context, prompt string) (string, error) {
	if b.client == nil {
		return "", fmt.Errorf("wau client not set; call WithClient first")
	}
	resp, err := b.client.Tasks().Submit(ctx, wau.SubmitRequest{
		Prompt:    prompt,
		TimeoutMs: defaultTimeoutMs,
	})
	if err != nil {
		return "", fmt.Errorf("botfeishu: %s", userFacingErrPrefix)
	}
	return resp.TaskID, nil
}

// --- helpers ---

// buildTextContent text → Feishu text msg_type content JSON:`{"text":"..."}`。
func buildTextContent(text string) string {
	b, _ := json.Marshal(map[string]string{"text": text})
	return string(b)
}

// extractTextContent 从 `{"text":"..."}` content 抽 text。
func extractTextContent(content string) string {
	var wrapper struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &wrapper); err != nil {
		return strings.TrimSpace(content)
	}
	return wrapper.Text
}

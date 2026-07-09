// Package botqq — QQ Bot SDK 集成(W6.2 Stage 1 native SDK integration, 2026-07-09)。
//
// Stage 1 native 实现(替换 W5 Stage 0 stub):
//   - OpenAPI:github.com/tencent-connect/botgo(NewOpenAPI + token.QQBotTokenSource)
//   - WSS Gateway 长连接:gorilla/websocket 直接 dial(api.WS() 拿 WSS URL)
//   - 事件解析:dto.WSPayload / dto.Message(MESSAGE_CREATE / AT / GROUP_AT / C2C)
//   - 公共 Bot interface 沿用 M10 N1 拍板(Start/Stop/OnMessage/WithTenant/WithUniverse)
//   - SubmitToCore 走 wau-core-kernel wau.Client.Tasks().Submit(per W7 模板)
//
// 协议合规(per wau-channel/adapter/qq/qq_real.go W7 2026-07-07 SDK 接通):
//   - D60 additive:老 bot/ 子包(telegram/discord/webhook)0 改动
//   - 公共 Bot interface 5 方法签名一字不改
package botqq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	botgo "github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/openapi"
	"github.com/tencent-connect/botgo/token"

	wau "github.com/wau/wau-go-sdk"
	botcommon "github.com/wau/wau-go-sdk/bot/common"
)

const (
	userFacingErrPrefix = "暂时无法回复,请稍后再试"
	defaultTimeoutMs    = 30000
)

// QQBot QQ Bot SDK 集成主结构(Stage 1 native)。
type QQBot struct {
	AppID     string
	AppSecret string
	Token     string
	Tenant    string
	Universe  string
	Handler   func(botcommon.IncomingMessage) botcommon.OutgoingMessage
	client    *wau.Client

	// native SDK 句柄(Start 时构造)
	api  openapi.OpenAPI
	conn *websocket.Conn

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	stopped chan struct{}
}

var _ botcommon.Bot = (*QQBot)(nil)

// NewQQBot 创建 QQ Bot 实例。
func NewQQBot(appID, appSecret, token string) *QQBot {
	return &QQBot{
		AppID:     appID,
		AppSecret: appSecret,
		Token:     token,
		stopped:   make(chan struct{}),
	}
}

// WithClient 注入 wau Client。
func (b *QQBot) WithClient(c *wau.Client) *QQBot {
	b.client = c
	return b
}

// WithTenant 设置 tenant_id。
func (b *QQBot) WithTenant(tenantID string) botcommon.Bot {
	b.Tenant = tenantID
	return b
}

// WithUniverse 设置 Universe 标签。
func (b *QQBot) WithUniverse(universe string) botcommon.Bot {
	b.Universe = universe
	return b
}

// OnMessage 注册消息处理 handler。
func (b *QQBot) OnMessage(handler func(botcommon.IncomingMessage) botcommon.OutgoingMessage) botcommon.Bot {
	b.Handler = handler
	return b
}

// Start 启动 QQ Bot(拉 WSS URL + dial + readPump)。
//
// 步骤:
//  1. 校验 appID/appSecret(0 门槛 UX)
//  2. token.NewQQBotTokenSource + botgo.NewOpenAPI
//  3. api.WS() 拿 WSS Gateway URL
//  4. gorilla/websocket dial + 后台 readPump goroutine
func (b *QQBot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("qq bot already running")
	}
	if b.AppID == "" || b.AppSecret == "" {
		b.mu.Unlock()
		return fmt.Errorf("botqq: app_id/app_secret required")
	}
	ts := token.NewQQBotTokenSource(&token.QQBotCredentials{
		AppID:     b.AppID,
		AppSecret: b.AppSecret,
	})
	b.api = botgo.NewOpenAPI(b.AppID, ts)
	b.mu.Unlock()

	wsCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wssAP, err := b.api.WS(wsCtx, nil, "")
	if err != nil || wssAP == nil || wssAP.URL == "" {
		return fmt.Errorf("botqq: WS gateway fetch failed: %w", err)
	}

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	conn, _, err := dialer.Dial(wssAP.URL, nil)
	if err != nil {
		return fmt.Errorf("botqq: WSS dial failed: %w", err)
	}

	runCtx, runCancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.conn = conn
	b.cancel = runCancel
	b.running = true
	b.mu.Unlock()

	go b.readPump(runCtx, conn)
	return nil
}

// readPump 后台读 WSS 帧 → parse → dispatch。
func (b *QQBot) readPump(ctx context.Context, conn *websocket.Conn) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[botqq] readPump panic: %v", r)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var payload dto.WSPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			continue
		}
		payload.RawMessage = data
		b.dispatch(ctx, &payload)
	}
}

// dispatch 归一化 WSS payload → IncomingMessage → Handler → PostMessage 回复。
func (b *QQBot) dispatch(ctx context.Context, p *dto.WSPayload) {
	if b.Handler == nil {
		return
	}
	var env struct {
		D json.RawMessage `json:"d"`
	}
	if err := json.Unmarshal(p.RawMessage, &env); err != nil || len(env.D) == 0 {
		return
	}
	switch p.Type {
	case dto.EventMessageCreate, dto.EventAtMessageCreate, dto.EventGroupAtMessageCreate, dto.EventC2CMessageCreate:
	default:
		return
	}
	var msg dto.Message
	if err := json.Unmarshal(env.D, &msg); err != nil {
		return
	}
	channelID := msg.ChannelID
	if p.Type == dto.EventGroupAtMessageCreate && msg.GroupID != "" {
		channelID = msg.GroupID
	}
	in := botcommon.IncomingMessage{
		PlatformMsgID: msg.ID,
		ChannelID:     channelID,
		Text:          msg.Content,
		Timestamp:     time.Now(),
	}
	if msg.Author != nil {
		in.UserID = msg.Author.ID
		in.Username = msg.Author.Username
	}
	out := b.Handler(in)
	if out.Text == "" || channelID == "" {
		return
	}
	if _, err := b.PostMessage(ctx, channelID, out.Text); err != nil {
		return
	}
}

// Stop 优雅停止。
func (b *QQBot) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running {
		return nil
	}
	b.running = false
	if b.cancel != nil {
		b.cancel()
	}
	if b.conn != nil {
		_ = b.conn.Close()
	}
	close(b.stopped)
	return nil
}

// PostMessage 用 botgo OpenAPI.PostMessage 发消息,返回 message ID。
func (b *QQBot) PostMessage(ctx context.Context, channelID, text string) (string, error) {
	if !b.running || b.api == nil {
		return "", fmt.Errorf("qq bot not running")
	}
	sent, err := b.api.PostMessage(ctx, channelID, &dto.MessageToCreate{
		Content: text,
		MsgType: dto.TextMsg,
	})
	if err != nil || sent == nil {
		return "", fmt.Errorf("botqq: %s", userFacingErrPrefix)
	}
	return sent.ID, nil
}

// UpdateMessage 用 botgo OpenAPI.PatchMessage 编辑消息(streaming edit 用)。
func (b *QQBot) UpdateMessage(ctx context.Context, channelID, msgID, newText string) error {
	if !b.running || b.api == nil {
		return fmt.Errorf("qq bot not running")
	}
	if _, err := b.api.PatchMessage(ctx, channelID, msgID, &dto.MessageToCreate{
		Content: newText,
		MsgType: dto.TextMsg,
	}); err != nil {
		return fmt.Errorf("botqq: %s", userFacingErrPrefix)
	}
	return nil
}

// SubmitToCore 走 wau-core-kernel 提交 prompt(per W7 模板)。
func (b *QQBot) SubmitToCore(ctx context.Context, prompt string) (string, error) {
	if b.client == nil {
		return "", fmt.Errorf("wau client not set; call WithClient first")
	}
	resp, err := b.client.Tasks().Submit(ctx, wau.SubmitRequest{
		Prompt:    prompt,
		TimeoutMs: defaultTimeoutMs,
	})
	if err != nil {
		return "", fmt.Errorf("botqq: %s", userFacingErrPrefix)
	}
	return resp.TaskID, nil
}

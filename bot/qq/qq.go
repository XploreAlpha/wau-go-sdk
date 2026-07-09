// Package botqq — QQ Bot SDK 集成(W5 OSS-onboarding closure, 2026-07-13)。
//
// 完整 SDK 端实现(per W5 Q1=B 反 W4.1 拍板):
//   - Stage 0 stub(per Python/TS/Rust telegram 模式)
//   - Stage 1 路径:接入 github.com/tencent-connect/botgo(openapi + dto + token) + gorilla/websocket
//   - 公共 Bot interface 沿用 M10 N1 拍板(Start/Stop/OnMessage/WithTenant/WithUniverse)
//   - 仍走 wau-edge POST /v1/bots/{bot_id}/messages 注册(per M10 N3)
package botqq

import (
	"context"
	"fmt"
	"sync"

	wau "github.com/wau/wau-go-sdk"
	botcommon "github.com/wau/wau-go-sdk/bot/common"
)

const (
	userFacingErrPrefix = "暂时无法回复,请稍后再试"
	defaultTimeoutMs    = 30000
)

// QQBot QQ Bot SDK 集成主结构。
type QQBot struct {
	AppID     string
	AppSecret string
	Token     string
	Tenant    string
	Universe  string
	Handler   func(botcommon.IncomingMessage) botcommon.OutgoingMessage
	client    *wau.Client
	mu        sync.Mutex
	running   bool
	stopped   chan struct{}
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

// Start 启动 QQ Bot(Stage 0 stub)。
func (b *QQBot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("qq bot already running")
	}
	b.running = true
	b.mu.Unlock()
	// Stage 1: botgo.New + websocket
	return nil
}

// Stop 优雅停止。
func (b *QQBot) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running {
		return nil
	}
	b.running = false
	close(b.stopped)
	return nil
}

// PostMessage 公开方法(Stage 0 stub)。
func (b *QQBot) PostMessage(ctx context.Context, channelID, text string) (string, error) {
	if !b.running {
		return "", fmt.Errorf("qq bot not running")
	}
	// Stage 1: botgo.PostMessage
	return "qq.mock.msg." + channelID, nil
}

// UpdateMessage 公开方法(Stage 0 stub)。
func (b *QQBot) UpdateMessage(ctx context.Context, channelID, msgID, newText string) error {
	if !b.running {
		return fmt.Errorf("qq bot not running")
	}
	// Stage 1: botgo.UpdateMessage
	return nil
}

// SubmitToCore 走 wau-core-kernel 提交 prompt。
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

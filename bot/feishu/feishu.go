// Package botfeishu — Feishu/Lark Bot SDK 集成(W5 OSS-onboarding closure, 2026-07-13)。
//
// 完整 SDK 端实现(per W5 Q1=B 反 W4.1 拍板):
//   - Stage 0 stub(per Python/TS/Rust telegram 模式)
//   - Stage 1 路径:接入 github.com/larksuite/oapi-sdk-go/v3(larkcore + larkws + larkim + dispatcher)
//   - 公共 Bot interface 沿用 M10 N1 拍板(Start/Stop/OnMessage/WithTenant/WithUniverse)
//   - 仍走 wau-edge POST /v1/bots/{bot_id}/messages 注册(per M10 N3)
//
// 字段对齐 per D13 拍板:与 wau-channel/adapter + 4 SDK bot/common/ 100% 一致。
package botfeishu

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

// FeishuBot Feishu/Lark Bot SDK 集成主结构。
type FeishuBot struct {
	AppID     string
	AppSecret string
	Tenant    string
	Universe  string
	Handler   func(botcommon.IncomingMessage) botcommon.OutgoingMessage
	client    *wau.Client
	mu        sync.Mutex
	running   bool
	stopped   chan struct{}
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

// Start 启动 Feishu WS(Stage 0 stub)。
func (b *FeishuBot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("feishu bot already running")
	}
	b.running = true
	b.mu.Unlock()
	// Stage 1: larkws.NewClient + dispatcher.RegisterHandler
	return nil
}

// Stop 优雅停止。
func (b *FeishuBot) Stop(ctx context.Context) error {
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
func (b *FeishuBot) PostMessage(ctx context.Context, chatID, text string) (string, error) {
	if !b.running {
		return "", fmt.Errorf("feishu bot not running")
	}
	// Stage 1: larkim.NewMessage + Send
	return "feishu.mock.msg." + chatID, nil
}

// UpdateMessage 公开方法(Stage 0 stub)。
func (b *FeishuBot) UpdateMessage(ctx context.Context, chatID, msgID, newText string) error {
	if !b.running {
		return fmt.Errorf("feishu bot not running")
	}
	// Stage 1: larkim.UpdateMessage
	return nil
}

// SubmitToCore 走 wau-core-kernel 提交 prompt(per W4.1 公共契约)。
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

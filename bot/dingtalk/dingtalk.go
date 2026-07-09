// Package botdingtalk — DingTalk Bot SDK 集成(W5 OSS-onboarding closure, 2026-07-13)。
//
// 完整 SDK 端实现(per W5 Q1=B 反 W4.1 拍板):
//   - Stage 0 stub(per Python/TS/Rust telegram 模式)
//   - Stage 1 路径:接入 github.com/open-dingtalk/dingtalk-stream-sdk-go/client + .../chatbot
//   - 公共 Bot interface 沿用 M10 N1 拍板(Start/Stop/OnMessage/WithTenant/WithUniverse)
package botdingtalk

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

// DingTalkBot DingTalk Bot SDK 集成主结构。
type DingTalkBot struct {
	AppKey    string
	AppSecret string
	RobotCode string
	Tenant    string
	Universe  string
	Handler   func(botcommon.IncomingMessage) botcommon.OutgoingMessage
	client    *wau.Client
	mu        sync.Mutex
	running   bool
	stopped   chan struct{}
}

var _ botcommon.Bot = (*DingTalkBot)(nil)

// NewDingTalkBot 创建 DingTalk Bot 实例。
func NewDingTalkBot(appKey, appSecret, robotCode string) *DingTalkBot {
	return &DingTalkBot{
		AppKey:    appKey,
		AppSecret: appSecret,
		RobotCode: robotCode,
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

// Start 启动 DingTalk Stream Mode(Stage 0 stub)。
func (b *DingTalkBot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("dingtalk bot already running")
	}
	b.running = true
	b.mu.Unlock()
	// Stage 1: dingtalk-stream.NewStreamClient
	return nil
}

// Stop 优雅停止。
func (b *DingTalkBot) Stop(ctx context.Context) error {
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
func (b *DingTalkBot) PostMessage(ctx context.Context, conversationID, text string) (string, error) {
	if !b.running {
		return "", fmt.Errorf("dingtalk bot not running")
	}
	// Stage 1: chatbot.SendMessage
	return "dingtalk.mock.msg." + conversationID, nil
}

// UpdateMessage 公开方法(Stage 0 stub)。
func (b *DingTalkBot) UpdateMessage(ctx context.Context, conversationID, msgID, newText string) error {
	if !b.running {
		return fmt.Errorf("dingtalk bot not running")
	}
	// Stage 1: chatbot.UpdateMessage
	return nil
}

// SubmitToCore 走 wau-core-kernel 提交 prompt。
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

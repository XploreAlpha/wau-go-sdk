// Package botemail — Email Bot SDK 集成(W5 OSS-onboarding closure, 2026-07-13)。
//
// 完整 SDK 端实现(per W5 Q1=B 反 W4.1 拍板):
//   - Stage 0 stub(per Python/TS/Rust telegram 模式)
//   - Stage 1 路径:接入 github.com/emersion/go-imap v1 + stdlib net/smtp
//   - 公共 Bot interface 沿用 M10 N1 拍板(Start/Stop/OnMessage/WithTenant/WithUniverse)
package botemail

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

// EmailBot Email Bot SDK 集成主结构。
type EmailBot struct {
	IMAPHost   string
	IMAPPort   int
	IMAPUser   string
	IMAPPass   string
	SMTPHost   string
	SMTPPort   int
	SMTPUser   string
	SMTPPass   string
	Tenant     string
	Universe   string
	Handler    func(botcommon.IncomingMessage) botcommon.OutgoingMessage
	client     *wau.Client
	mu         sync.Mutex
	running    bool
	stopped    chan struct{}
}

var _ botcommon.Bot = (*EmailBot)(nil)

// NewEmailBot 创建 Email Bot 实例。
func NewEmailBot(imapHost string, imapPort int, imapUser, imapPass, smtpHost string, smtpPort int, smtpUser, smtpPass string) *EmailBot {
	return &EmailBot{
		IMAPHost: imapHost,
		IMAPPort: imapPort,
		IMAPUser: imapUser,
		IMAPPass: imapPass,
		SMTPHost: smtpHost,
		SMTPPort: smtpPort,
		SMTPUser: smtpUser,
		SMTPPass: smtpPass,
		stopped:  make(chan struct{}),
	}
}

// WithClient 注入 wau Client。
func (b *EmailBot) WithClient(c *wau.Client) *EmailBot {
	b.client = c
	return b
}

// WithTenant 设置 tenant_id。
func (b *EmailBot) WithTenant(tenantID string) botcommon.Bot {
	b.Tenant = tenantID
	return b
}

// WithUniverse 设置 Universe 标签。
func (b *EmailBot) WithUniverse(universe string) botcommon.Bot {
	b.Universe = universe
	return b
}

// OnMessage 注册消息处理 handler。
func (b *EmailBot) OnMessage(handler func(botcommon.IncomingMessage) botcommon.OutgoingMessage) botcommon.Bot {
	b.Handler = handler
	return b
}

// Start 启动 Email IMAP IDLE(Stage 0 stub)。
func (b *EmailBot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("email bot already running")
	}
	b.running = true
	b.mu.Unlock()
	// Stage 1: imap client + IDLE command
	return nil
}

// Stop 优雅停止。
func (b *EmailBot) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running {
		return nil
	}
	b.running = false
	close(b.stopped)
	return nil
}

// SendMessage 公开方法(Stage 0 stub,Email 用 SendMessage 而非 PostMessage)。
func (b *EmailBot) SendMessage(ctx context.Context, to, subject, body string) error {
	if !b.running {
		return fmt.Errorf("email bot not running")
	}
	// Stage 1: smtp.SendMail
	return nil
}

// UpdateMessage 公开方法(Stage 0 stub,Email 无 update 概念,返回 mock)。
func (b *EmailBot) UpdateMessage(ctx context.Context, messageID, newBody string) error {
	if !b.running {
		return fmt.Errorf("email bot not running")
	}
	// Email 不支持更新已发邮件,此方法为兼容接口
	return nil
}

// SubmitToCore 走 wau-core-kernel 提交 prompt。
func (b *EmailBot) SubmitToCore(ctx context.Context, prompt string) (string, error) {
	if b.client == nil {
		return "", fmt.Errorf("wau client not set; call WithClient first")
	}
	resp, err := b.client.Tasks().Submit(ctx, wau.SubmitRequest{
		Prompt:    prompt,
		TimeoutMs: defaultTimeoutMs,
	})
	if err != nil {
		return "", fmt.Errorf("botemail: %s", userFacingErrPrefix)
	}
	return resp.TaskID, nil
}

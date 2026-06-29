// Package botcommon — BotBuilder 通用 builder(per `feedback-dev-style` 偏好 builder)。
//
// Stage 0 脚手架:基本 builder 骨架。
// Stage 1 M1 子项 6 实装 BotBuilder 完整选项 + 内部状态管理。
package botcommon

import "context"

// BotBuilder 通用 builder(per `feedback-dev-style`)。
//
// 用法:
//
//	bot := botcommon.NewBuilder().
//	    WithTenant("acme").
//	    WithUniverse("us-prod").
//	    OnMessage(func(msg botcommon.IncomingMessage) botcommon.OutgoingMessage {
//	        return botcommon.OutgoingMessage{Text: "echo: " + msg.Text}
//	    }).
//	    Build()
type BotBuilder struct {
	tenantID  string
	universe  string
	handler   func(IncomingMessage) OutgoingMessage
	startCtx  context.Context
	stopCtx   context.Context
}

// NewBuilder 创建 BotBuilder。
func NewBuilder() *BotBuilder {
	return &BotBuilder{}
}

// WithTenant 设置 tenant_id。
func (b *BotBuilder) WithTenant(tenantID string) *BotBuilder {
	b.tenantID = tenantID
	return b
}

// WithUniverse 设置 Universe 标签。
func (b *BotBuilder) WithUniverse(universe string) *BotBuilder {
	b.universe = universe
	return b
}

// OnMessage 注册消息处理 handler。
func (b *BotBuilder) OnMessage(handler func(IncomingMessage) OutgoingMessage) *BotBuilder {
	b.handler = handler
	return b
}

// TenantID 返回已设置的 tenant_id(供具体 adapter 读取)。
func (b *BotBuilder) TenantID() string { return b.tenantID }

// Universe 返回已设置的 universe。
func (b *BotBuilder) Universe() string { return b.universe }

// Handler 返回已注册的 handler。
func (b *BotBuilder) Handler() func(IncomingMessage) OutgoingMessage { return b.handler }

// Build 是占位入口(Stage 1 雏形期具体 adapter 实现)。
//
// Stage 0:返回 nil(没有具体 Bot)。
// Stage 1:各 platform(telegram/discord/webhook) 的 New() 函数用 BotBuilder 构建具体 Bot。
func (b *BotBuilder) Build() Bot {
	// TODO(stage1-m1): 具体 adapter 接管 Build()
	return nil
}

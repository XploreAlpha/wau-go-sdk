// Package botcommon — 4 SDK 公共 Bot interface(per D13 拍板:完全统一)。
//
// Bot 通用 Bot 接口,所有 SDK 必须实现同样的方法签名。
package botcommon

import "context"

// Bot 通用 Bot 接口(per D13)。
//
// 4 SDK 必须实现的方法签名 100% 一致:
//   - Start(ctx) error
//   - Stop(ctx) error
//   - OnMessage(handler) Bot
//   - WithTenant(tenantID) Bot
//   - WithUniverse(universe) Bot
type Bot interface {
	// Start 启动 bot(长连接 / webhook server)。
	Start(ctx context.Context) error

	// Stop 优雅停止。
	Stop(ctx context.Context) error

	// OnMessage 注册消息处理 handler,返回 Bot 支持链式调用。
	OnMessage(handler func(IncomingMessage) OutgoingMessage) Bot

	// WithTenant 设置 tenant_id,返回 Bot 支持链式调用。
	WithTenant(tenantID string) Bot

	// WithUniverse 设置 Universe 标签(W-6),返回 Bot 支持链式调用。
	WithUniverse(universe string) Bot
}

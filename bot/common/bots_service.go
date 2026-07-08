// Package botcommon — BotsService 公共 interface(per M10 N1 / D82=A)
//
// 4 SDK 必须保持签名 100% 一致(per D13):
//   - Register(ctx, req) (*Account, error)
//   - Get(ctx, publicBotID) (*Account, error)
//   - Update(ctx, publicBotID, req) (*Account, error)
//   - List(ctx, filter) ([]*Account, error)
//   - Delete(ctx, publicBotID) error
package botcommon

import (
	"context"
	"errors"
)

// BotsService M10 bot 注册中心客户端接口。
//
// 调用方:B 端 SDK 上架工具(wau-cli)、wau-edge 路由层、wau-agent 启动时 bot 列表拉取。
// 实现方:每个 SDK 的 `bot/registry/` 子包,通常走 HTTP POST /registry/bots。
//
// 错误语义(per D60):
//   - bot 不存在:Get/Update/Delete → 返回 wrapped error(caller 用 errors.Is / errors.As 判别)
//   - 参数错误:返回 wrapped error
//   - 网络错误:返回底层 transport error
type BotsService interface {
	// Register 注册一个新 bot。
	// 服务端分配 AccountID + CreatedAt / UpdatedAt,客户端不传。
	// 冲突(TenantID + BotID 已存在):服务端返 409,caller 处理。
	Register(ctx context.Context, req RegisterBotRequest) (*Account, error)

	// Get 按公开 ID 获取 bot 信息。
	// 不存在 → 返回 (nil, ErrBotNotFound)。
	Get(ctx context.Context, publicBotID string) (*Account, error)

	// Update 更新 bot 的可变字段(OwnerUserID / ChannelType / ChannelConfigID)。
	// TenantID / BotID / PublicBotID 不可改。
	// 不存在 → ErrBotNotFound。
	Update(ctx context.Context, publicBotID string, req UpdateBotRequest) (*Account, error)

	// List 按 filter 列出 bot 信息。
	// filter 留空 → 列所有 caller 有权限看的(per B 端 RBAC)。
	List(ctx context.Context, filter ListBotsFilter) ([]*Account, error)

	// Delete 按公开 ID 注销 bot。
	// 不存在 → ErrBotNotFound。
	Delete(ctx context.Context, publicBotID string) error
}

// 公共 sentinel errors(实现方在 wrap 时保留 Is 链)。
var (
	// ErrBotNotFound bot 不存在(Get/Update/Delete 时)。
	ErrBotNotFound = errors.New("botcommon: bot not found")

	// ErrBotAlreadyExists 注册冲突(TenantID + BotID 已存在)。
	ErrBotAlreadyExists = errors.New("botcommon: bot already exists")

	// ErrInvalidArgument 参数错误(空字段、非法 channel_type 等)。
	ErrInvalidArgument = errors.New("botcommon: invalid argument")
)

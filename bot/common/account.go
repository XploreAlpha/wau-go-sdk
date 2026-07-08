// Package botcommon — Account / BotRegistry 公共 DTO(per M10 / D82=A)
//
// 公开 bot id 格式:"bot:<tenant>:<botid>"
// 例:tenant=acme, botID=weather-cn → PublicBotID="bot:acme:weather-cn"
//
// 4 SDK 必须保持字段名 + 类型 100% 一致(per D13 拍板)。
// wau-python-sdk/wau_sdk/bot/common/account.py
// wau-typescript-sdk/src/bot/common/account.ts
// wau-rust-sdk/src/bot/common/account.rs
// 必须随时保持同步,字段一字不差。
package botcommon

import (
	"fmt"
	"time"
)

// Account B 端注册的 bot 账户(per M10 N1 / D82=A 拍板)。
//
// 注:此 Account 是 bot 注册中心(wau-registry-service)入口的领域对象,
//不是 wau-store 的 Account(财务账户),两者不应混用。
type Account struct {
	// AccountID registry 服务端分配的 UUID(空 = 待注册,服务端回填)
	AccountID string `json:"account_id"`

	// TenantID 多租户 ID(必填,例 "acme")
	TenantID string `json:"tenant_id"`

	// BotID 本地名 / slug(必填,例 "weather-cn")。在 tenant 内唯一
	BotID string `json:"bot_id"`

	// PublicBotID 全局公开 ID(由 TenantID + BotID 派生,D82=A)
	// 例:tenant=acme + bot=weather-cn → "bot:acme:weather-cn"
	// 服务端写入时校验一致性,客户端只读
	PublicBotID string `json:"public_bot_id"`

	// OwnerUserID 注册人 user_id(C 端 或 B 端 owner,谁注册谁负责)
	OwnerUserID string `json:"owner_user_id"`

	// ChannelType 绑定的 IM 平台类型(per M5 八平台):
	// "telegram"|"discord"|"slack"|"feishu"|"dingtalk"|"qq"|"email"|"webhook"
	ChannelType string `json:"channel_type"`

	// ChannelConfigID wau-channel 内的 config ID(platform credentials 索引)
	ChannelConfigID string `json:"channel_config_id"`

	// CreatedAt 注册时间(UTC,服务端回填,客户端只读)
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt 最后更新时间(UTC,服务端回填,客户端只读)
	UpdatedAt time.Time `json:"updated_at"`
}

// NewAccount 构造 Account 并填充 PublicBotID(D82=A 规则)。
//
// AccountID / CreatedAt / UpdatedAt 留空 → 服务端 Register 时回填。
// 这样客户端只声明业务字段,服务端掌控元数据。
func NewAccount(tenantID, botID, ownerUserID, channelType, channelConfigID string) *Account {
	now := time.Now().UTC()
	return &Account{
		TenantID:        tenantID,
		BotID:           botID,
		PublicBotID:     fmt.Sprintf("bot:%s:%s", tenantID, botID),
		OwnerUserID:     ownerUserID,
		ChannelType:     channelType,
		ChannelConfigID: channelConfigID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// PublicBotIDOf 给定 tenant + botID 计算公开 ID(纯函数,无副作用)。
//
// 等价于 fmt.Sprintf("bot:%s:%s", tenantID, botID)。
// SDK 任何地方需要派生公开 ID 都走这个,避免散落 Sprintf。
func PublicBotIDOf(tenantID, botID string) string {
	return fmt.Sprintf("bot:%s:%s", tenantID, botID)
}

// RegisterBotRequest B 端注册 bot 的请求体(POST /registry/bots)。
//
// 与 Account 区别:请求体不带 AccountID / CreatedAt / UpdatedAt
// (这些是服务端回填的字段)。
type RegisterBotRequest struct {
	TenantID        string `json:"tenant_id"`
	BotID           string `json:"bot_id"`
	OwnerUserID     string `json:"owner_user_id"`
	ChannelType     string `json:"channel_type"`
	ChannelConfigID string `json:"channel_config_id"`
}

// UpdateBotRequest B 端更新 bot 的请求体(PUT /registry/bots/{public_bot_id})。
//
// 只允许改 OwnerUserID / ChannelType / ChannelConfigID(账户身份 + 绑定)。
// TenantID / BotID / PublicBotID 都是 immutable 字段。
type UpdateBotRequest struct {
	OwnerUserID     string `json:"owner_user_id,omitempty"`
	ChannelType     string `json:"channel_type,omitempty"`
	ChannelConfigID string `json:"channel_config_id,omitempty"`
}

// ListBotsFilter 列举 bot 时的过滤条件(GET /registry/bots)。
type ListBotsFilter struct {
	TenantID    string `json:"tenant_id,omitempty"`
	OwnerUserID string `json:"owner_user_id,omitempty"`
	ChannelType string `json:"channel_type,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// Package botcommon 定义 4 SDK 共享的通用消息类型(per D13 拍板:4 SDK 完全统一)。
//
// 字段名 + 类型必须与 wau-python-sdk/wau-typescript-sdk/wau-rust-sdk 100% 一致。
package botcommon

import "time"

// IncomingMessage 收到用户消息(per D13 与 wau-channel/adapter/adapter.go 对齐)。
type IncomingMessage struct {
	PlatformMsgID string
	ChannelID     string
	UserID        string
	Username      string
	Text          string
	Attachments   []Attachment
	ReplyTo       string
	Timestamp     time.Time
}

// OutgoingMessage 发送给用户消息。
type OutgoingMessage struct {
	Text        string
	Attachments []Attachment
	ReplyTo     string
}

// Attachment 通用附件类型(per D13 Attachment.Type 取值 "image"/"file"/"audio"/"video")。
type Attachment struct {
	Type string
	URL  string
	Name string
}

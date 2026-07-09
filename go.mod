module github.com/wau/wau-go-sdk

go 1.23

require (
	github.com/XploreAlpha/circuit v0.6.0
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
)

// W6.1 (2026-07-09) Stage 1 dep 追加 — 5 平台 SDK native integration
// 沿用 wau-channel 8 平台 adapter 模板 (W7 2026-07-07 SDK 接通)
require (
	github.com/slack-go/slack v0.15.0                       // Slack socketmode + RTM
	github.com/larksuite/oapi-sdk-go/v3 v3.9.8              // Feishu/Lark EventReceiver
	github.com/tencent-connect/botgo v0.2.1                 // QQ OpenAPI v2 websocket
	github.com/open-dingtalk/dingtalk-stream-sdk-go v0.9.1  // DingTalk StreamCallback
	github.com/emersion/go-imap v1.2.1                      // Email IMAP IDLE (SMTP 用 stdlib net/smtp)
)

require (
	github.com/bwmarrin/discordgo v0.29.0 // indirect
	github.com/go-telegram-bot-api/telegram-bot-api/v5 v5.5.1 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	golang.org/x/crypto v0.0.0-20210421170649-83a5a9bb288b // indirect
	golang.org/x/sys v0.0.0-20201119102817-f84b799fce68 // indirect
)

replace github.com/XploreAlpha/circuit => ../wau-circuit

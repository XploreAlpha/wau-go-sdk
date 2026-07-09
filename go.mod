module github.com/wau/wau-go-sdk

go 1.25

require (
	github.com/XploreAlpha/circuit v0.6.0
	github.com/bwmarrin/discordgo v0.29.0
	github.com/go-telegram-bot-api/telegram-bot-api/v5 v5.5.1
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
)

// W6.1 (2026-07-09) Stage 1 dep 追加 — 5 平台 SDK native integration
// 沿用 wau-channel 8 平台 adapter 模板 (W7 2026-07-07 SDK 接通)
require (
	github.com/emersion/go-imap v1.2.1 // Email IMAP IDLE (SMTP 用 stdlib net/smtp)
	github.com/larksuite/oapi-sdk-go/v3 v3.9.8 // Feishu/Lark EventReceiver
	github.com/open-dingtalk/dingtalk-stream-sdk-go v0.9.1 // DingTalk StreamCallback
	github.com/slack-go/slack v0.27.0 // Slack socketmode + RTM (W6.2: 对齐 module cache / wau-channel,v0.15 无 socketmode)
	github.com/tencent-connect/botgo v0.2.1 // QQ OpenAPI v2 websocket
)

require (
	github.com/emersion/go-sasl v0.0.0-20200509203442-7bfe0ed36a21 // indirect
	github.com/go-resty/resty/v2 v2.6.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/tidwall/gjson v1.9.3 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	golang.org/x/crypto v0.16.0 // indirect
	golang.org/x/net v0.19.0 // indirect
	golang.org/x/oauth2 v0.23.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)

replace github.com/XploreAlpha/circuit => ../wau-circuit

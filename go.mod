module github.com/wau/wau-go-sdk

go 1.23

require (
	github.com/XploreAlpha/circuit v0.6.0
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
)

require (
	github.com/bwmarrin/discordgo v0.29.0 // indirect
	github.com/go-telegram-bot-api/telegram-bot-api/v5 v5.5.1 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	golang.org/x/crypto v0.0.0-20210421170649-83a5a9bb288b // indirect
	golang.org/x/sys v0.0.0-20201119102817-f84b799fce68 // indirect
)

replace github.com/XploreAlpha/circuit => ../wau-circuit

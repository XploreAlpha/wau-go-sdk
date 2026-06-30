// 示例:Telegram Bot 5 行接入范本(per M1 §2.8 雏形,2026-06-28 youhaoxi;
// v0.9.0 升级 2026-06-30 走 wau-edge OpenAI 兼容层,per M3 §3.7 H1+H2 拍板)。
//
// 跑法:
//
//	export WAU_BOT_TOKEN="your-telegram-bot-token-from-botfather"
//	export WAU_EDGE_URL="http://localhost:18402"  # wau-edge 端口(不是 wau-core 18400)
//	cd examples/bot_telegram && go run main.go
//
// 期望:启动后,用户给 bot 发消息 → bot 通过 wau-edge /v1/chat/completions 拿回复 → Reply 用户
//
// 完整链路(per M3 §4.5.1):
//   telegram bot → wau-edge :18402 /v1/chat/completions
//                → wau-llm-router :18403 /v1/resolve(决定 userToken + model)
//                → new-api :3000 /v1/chat/completions → LLM provider
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	tgbot "github.com/wau/wau-go-sdk/bot/telegram"
	wau "github.com/wau/wau-go-sdk"
	botcommon "github.com/wau/wau-go-sdk/bot/common"
)

func main() {
	token := os.Getenv("WAU_BOT_TOKEN")
	edgeURL := os.Getenv("WAU_EDGE_URL")
	if edgeURL == "" {
		edgeURL = "http://localhost:18402" // wau-edge 默认端口
	}
	if token == "" {
		log.Fatal("请设置环境变量 WAU_BOT_TOKEN")
	}

	// 1. 创建 wau-go-sdk Client(走 wau-edge OpenAI 兼容层,per D20)
	c, err := wau.New(edgeURL, wau.WithTimeout(30*time.Second))
	if err != nil {
		log.Fatalf("wau.New: %v", err)
	}
	defer c.Close()

	// 2. 构造 bot(5 行核心:Client + token + builder)
	builder := botcommon.NewBuilder().
		WithTenant("demo-tenant").
		WithUniverse("default")
	universe := builder.Universe() // 在闭包外捕获,handler 用
	bot := tgbot.New(token, builder.
		OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
			// 3. 调 wau-edge OpenAI 兼容层拿回复(per M3 §3.7)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			resp, err := c.Chat().Completions(ctx, wau.ChatCompletionRequest{
				Model: "gpt-4o-mini",
				Messages: []wau.ChatMessage{
					{Role: "system", Content: "你是一个 helpful assistant,回答简洁。"},
					{Role: "user", Content: in.Text},
				},
				Universe: universe, // 从 builder.WithUniverse 透传
			})
			if err != nil || len(resp.Choices) == 0 {
				return botcommon.OutgoingMessage{Text: "暂时无法回复,请稍后再试"}
			}
			return botcommon.OutgoingMessage{Text: resp.Choices[0].Message.Content}
		}))

	// 4. 启动 + 等待信号
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bot.Start(ctx); err != nil {
		log.Fatalf("bot.Start: %v", err)
	}
	log.Printf("Bot 启动成功,等待消息... (Ctrl+C 退出)")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	// 5. 优雅停止
	_ = bot.Stop(context.Background())
	log.Printf("Bot 已停止")
}

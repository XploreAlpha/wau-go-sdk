// 示例:Discord Bot 5 行接入范本(per M1 §2.9 雏形,2026-06-28 youhaoxi)。
//
// 跑法:
//
//	export WAU_BOT_TOKEN="your-discord-bot-token"
//	export WAU_CORE_URL="http://localhost:18400"
//	cd examples/bot_discord && go run main.go
//
// 期望:启动后,用户在 Discord @ bot 发消息 → bot 通过 wau-core 提交 task → 拿到回复 → Reply
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	botdis "github.com/wau/wau-go-sdk/bot/discord"
	wau "github.com/wau/wau-go-sdk"
	botcommon "github.com/wau/wau-go-sdk/bot/common"
)

func main() {
	token := os.Getenv("WAU_BOT_TOKEN")
	coreURL := os.Getenv("WAU_CORE_URL")
	if token == "" || coreURL == "" {
		log.Fatal("请设置环境变量 WAU_BOT_TOKEN 和 WAU_CORE_URL")
	}

	// 1. 创建 wau-go-sdk Client(走 wau-core)
	c, err := wau.New(coreURL, wau.WithTimeout(30*time.Second))
	if err != nil {
		log.Fatalf("wau.New: %v", err)
	}
	defer c.Close()

	// 2. 构造 bot(5 行核心:Client + token + builder)
	bot := botdis.New(token, botcommon.NewBuilder().
		WithTenant("demo-tenant").
		WithUniverse("default").
		OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
			// 3. 调 wau-core 拿回复
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			resp, err := c.Tasks().Submit(ctx, wau.SubmitRequest{
				Prompt:    in.Text,
				TimeoutMs: 30000,
			})
			if err != nil || resp.Error != "" {
				return botcommon.OutgoingMessage{Text: "暂时无法回复,请稍后再试"}
			}
			return botcommon.OutgoingMessage{Text: fmt.Sprintf("%v", resp.Response)}
		}))

	// 4. 启动 + 等待信号
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bot.Start(ctx); err != nil {
		log.Fatalf("bot.Start: %v", err)
	}
	log.Printf("Discord Bot 启动成功,等待消息... (Ctrl+C 退出)")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	// 5. 优雅停止
	_ = bot.Stop(context.Background())
	log.Printf("Discord Bot 已停止")
}

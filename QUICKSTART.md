# wau-go-sdk 15 分钟跑通

> 目标:Go module 安装 + 跑通 1 个 webhook bot + 收到消息触发响应。

## 前置

- Go 1.21+
- 上游:
  - **wau-llm-router** 在 :18404 跑(gRPC 直连模式)
  - 或 **wau-channel webhook** 在 :18431 跑(SDK 模式)
- Telegram token:`$TELEGRAM_BOT_TOKEN` env(per [[feedback-hf-token-leak-2026-06-17]])

## 步骤

### 1. 拉 SDK

```bash
cd ~/myproject
go get github.com/XploreAlpha/wau-go-sdk@v1.1.0
```

### 2. 写 5 行 bot

```bash
mkdir -p mybot && cd mybot
cat > main.go <<'EOF'
package main

import (
    "log"
    "github.com/XploreAlpha/wau-go-sdk/bot/telegram"
)

func main() {
    bot := telegram.New(telegram.Config{
        Token:    os.Getenv("TELEGRAM_BOT_TOKEN"),
        TenantID: "acme",
    })
    if err := bot.Start(); err != nil {
        log.Fatal(err)
    }
}
EOF
```

### 3. 跑

```bash
TELEGRAM_BOT_TOKEN=$TELEGRAM_BOT_TOKEN go run main.go
```

预期:`[telegram-bot] listening on :18431, tenant=acme`

### 4. 触发 1 条消息

打开 Telegram,跟你 bot 私聊发:"hello"
预期:bot 自动转 Task → wau-llm-router → wau-edge → LLM → 响应回 Telegram

## 直连模式(不走 webhook)

```go
import "github.com/XploreAlpha/wau-go-sdk/client"

c := client.New("127.0.0.1:18404")
resp, err := c.Resolve(ctx, &decision.ResolveInput{
    TenantID: "acme",
    Intent:   "chat",
})
```

## 下一步

- [DEPLOY.md](DEPLOY.md) — 版本管理 + go modules
- [ARCHITECTURE.md](ARCHITECTURE.md) — bot/ 字段 5/5 对齐细节
- [README.md](README.md) — v0.9.0 收口段

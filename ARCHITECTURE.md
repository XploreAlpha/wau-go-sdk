# wau-go-sdk 架构

## 模块拆分

```
wau-go-sdk/
├── client/                     # gRPC client → wau-llm-router
├── bot/
│   ├── adapter.go              # BotAdapter interface
│   ├── telegram/               # Telegram adapter
│   ├── discord/                # Discord adapter
│   └── webhook/                # Webhook adapter
├── examples/
│   └── bot-webhook/main.go     # 5 行 bot 例子
└── README.md / QUICKSTART.md / DEPLOY.md / ARCHITECTURE.md / CHANGELOG.md
```

## 数据流

### SDK 模式(B 端开发者首选)

```
B 端 app
    ↓ go get wau-go-sdk
    ↓ import bot/telegram
telegram.Bot.Start()
    ↓ goroutine 拉 / 推消息
Telegram Bot API
    ↓
wau-go-sdk/bot/telegram 内部转 A2A Task
    ↓ 注入 wau-channel webhook
wau-channel :18431 → wau-core-kernel → wau-llm-router → LLM
    ↓
响应回 Telegram
```

### 直连模式(性能敏感 / 内网)

```
B 端 app
    ↓ client.New("127.0.0.1:18404")
client.Resolve()
    ↓ gRPC :18404
wau-llm-router
    ↓
LLMDecision
    ↓
B 端 app 自己拿 userToken 调 new-api
```

## 关键决策(per §3.7 + 拍板)

| 决策 | 内容 |
|---|---|
| **bot/ 字段 5/5 对齐** | per [[project-v0-9-0-stage0-closure-2026-06-28]] |
| **26 funcs / 0 回归** | per [[project-v0-9-0-M3-§3.7-chat-sdk-4langs-2026-06-30]] |
| **Telegram / Discord / Webhook 3 平台** | 雏形 |

## 接口边界

- **入**:B 端 app
- **出**:bot 启动 / Resolve 返回值
- **依赖**:wau-channel / wau-llm-router(纯 client,无独立服务)
- **被依赖**:B 端 app

## 性能预算

| 指标 | 目标 |
|---|---|
| Resolve P50 | < 1 ms(直连模式)|
| Bot 启动 | < 100 ms |
| 消息处理 | < 50 ms(SDK 内)|

## 跟其他仓的关系

- **上游**:B 端 app
- **下游**:wau-channel / wau-llm-router
- **同组 SDK(per [[project-v0-9-0-stage0-closure-2026-06-28]])**:wau-python-sdk / wau-typescript-sdk / wau-rust-sdk

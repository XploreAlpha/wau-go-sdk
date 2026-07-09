# wau-go-sdk

> **WAU Go SDK v1.0.0 GA** — 官方 Go 客户端,WAU-core-kernel 智能调度内核接入入口
> v0.7.0 "Amber" 🔷 — **v1.0.0 = 2026-07-25 GA**(W7.7 完成)

[![Go Reference](https://pkg.go.dev/badge/github.com/wau/wau-go-sdk.svg)](https://pkg.go.dev/github.com/wau/wau-go-sdk)
[![Version](https://img.shields.io/badge/version-v1.0.0-blue?style=flat-square)](https://github.com/wau/wau-go-sdk/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

## 状态

✅ **v1.0.0 GA** (2026-07-25) — **Public API stable**

| Phase | 状态 |
|-------|------|
| W5.1 go.mod + CI + 基础脚手架 | ✅ |
| W5.2 wau-circuit 补单测(地基) | ✅ |
| W5.3 Client + Agents + Tasks + Kernel 4 核心对象 | ✅ |
| W5.4 熔断 + 重试装饰器 | ✅ |
| W5.5 HS256 鉴权 | ✅ |
| W5.6 单测 + 5 场景契约 + 真 kernel 联调 | ✅ |
| W5.7 README + docs + examples | ✅ |
| W5.8 OTel Tracer 抽象(W1 M3 Day 3) | ✅ |
| W1 Day 6 5 edge case 测试(timeout / rate-limit / cancel / retry / 5xx) | ✅ |
| W7.7 Public API stable + deprecation policy | ✅ |
| tag v1.0.0 + pkg.go.dev | ⏳ 用户手动 |

## 安装

```bash
go get github.com/wau/wau-go-sdk@v1.0.0
```

## Public API 速览

```go
client, _ := wau.New("http://localhost:18400",
    wau.WithTimeout(30*time.Second),
    wau.WithAuth("your-token"),
    wau.WithTracer(myTracer),  // 可选 OTel adapter
)

// 4 核心服务
client.Kernel().Info(ctx)              // GET /kernel/info
client.Agents().List(ctx)              // GET /registry/agents
client.Agents().Get(ctx, "Whis")       // GET /registry/agents/{name}
client.Tasks().Submit(ctx, req)        // POST /registry/tasks/submit
client.Intent().Parse(ctx, prompt)     // gRPC stub(M3.1 实装)
```

## 关联仓库

- 上游: [wau-core-kernel](https://github.com/wau/wau-core-kernel) (HTTP :18400, gRPC :50051)
- 兄弟: [wau-python-sdk](https://github.com/wau/wau-python-sdk) | [wau-typescript-sdk](https://github.com/wau/wau-typescript-sdk) | [wau-rust-sdk](https://github.com/wau/wau-rust-sdk) — 4/5 SDK 1.0 GA
- 抽取源: [wau-cli/internal/client/](https://github.com/wau/wau-cli/tree/main/internal/client) (337 行)
- 复用: [wau-circuit](https://github.com/wau/wau-circuit) (熔断器)

## 计划文档

v0.7.0 完整计划:[`/home/inamoto888/WAU-develop/develop-log/kernel/v0.7.0/milestone.md`](file:///home/inamoto888/WAU-develop/develop-log/kernel/v0.7.0/milestone.md)

## v0.9.0 "Acorn" 收口段(2026-09-15 GA)

上文介绍 v0.7.0 计划 + 协议。本段为 v0.9.0 GA 增量补充。

### 角色

| OS 类比 | Client SDK(Go,开发者入口)|
|---|---|
| 部署 | Go module,被 B 端开发者 `go get` |
| 通信 | gRPC → wau-llm-router :18404 + wau-channel webhook 模式 |
| 状态 | v1.1.0 已随 v0.8.0 GA 同步发版(2026-07-13)|

### v0.9.0 新增

- **直连 wau-llm-router**(per [[project-v0-9-0-M3-§3.7-chat-sdk-4langs-2026-06-30]]):不再经过 wau-edge 也能调
- **bot/ 字段 5/5 100% 对齐**(per [[project-v0-9-0-stage0-closure-2026-06-28]]):Telegram / Discord / Webhook
- **per §3.7 §6**:4 SDK 一致 26 funcs / 0 回归

### 5 行 SDK 用例

```go
import "github.com/XploreAlpha/wau-go-sdk/bot/telegram"

bot := telegram.New(telegram.Config{
    Token:   os.Getenv("TELEGRAM_TOKEN"),
    TenantID: "acme",
})
bot.Start() // 监听消息 + 自动转 Task
```

### v0.9.0 "Acorn" 5 份核心文档

| # | 文件 | 内容 |
|---|---|---|
| 1 | [README.md](README.md)(本文件)| SDK 入口 + 协议 |
| 2 | [QUICKSTART.md](QUICKSTART.md) | 15 分钟跑通第 1 个 bot |
| 3 | [DEPLOY.md](DEPLOY.md) | 发布 + 版本管理 |
| 4 | [ARCHITECTURE.md](ARCHITECTURE.md) | 模块 + 直连链路 |
| 5 | [CHANGELOG.md](CHANGELOG.md) | v0.7.0 + v1.1.0 倒序(136 行已存在)|

### 历史锚点

- v1.1.0 SDK 同步发版(per [[project-v0.8.0-GA-2026-07-13]])
- bot/ 字段 5/5 对齐(per [[project-v0-9-0-stage0-closure-2026-06-28]])

## 协议

MIT © 2026 youhaoxi
## Bot Platforms

WAU SDK 通过两段责任分工对接 N 个 Bot 平台:

| 责任段 | 仓 | 文件 | 覆盖范围 |
|---|---|---|---|
| **公共契约** | 本 SDK(`bot/common/bots_service.<ext>`) | `Bot` + `BotsService` 抽象接口 | 5 SDK 100% 一致(per M10 N1)|
| **C 端 SDK bot/ 子包** | 本 SDK | `bot/{telegram,discord,webhook,slack,feishu,qq,dingtalk,email}/` | 每个 SDK 自带 8 平台(W5 Q1=B 反 W4.1 拍板)|
| **服务端 8 平台 adapter** | `wau-channel` | `internal/adapter/{slack,feishu,dingtalk,qq,email,telegram,discord,webhook}/*_real.go` | 全部 8 平台完整 4 步(per W7 2026-07-07 SDK 接通)|
| **服务端 bot HTTP API** | `wau-edge` | `POST /v1/bots/{bot_id}/messages`(per M10 N3) | Bot → 后端路由 |

**Bot Platforms 公开能力表**(2026-07-13, **W5 update 反 W4.1**):

| Platform     | 本 SDK bot/ | wau-channel adapter | 状态 |
|--------------|-------------|---------------------|------|
| Telegram     | ✅ | ✅ | 双端完整 |
| Discord      | ✅ | ✅ | 双端完整 |
| Webhook      | ✅ | ✅ | 双端完整 |
| Slack        | ✅ Stage 1 native | ✅ 完整 4 步(`slack-go/slack` v0.27.0 socketmode + RTM) | W6.2 closure: socketmode client + RTM 监听 (~244 LoC) |
| Feishu       | ✅ Stage 1 native | ✅ 完整 4 步(`larksuite/oapi-sdk-go/v3` v3.9.8) | W6.2 closure: EventReceiver 集成 (~264 LoC) |
| QQ           | ✅ Stage 1 native | ✅ 完整 4 步(`tencent-connect/botgo` v0.2.1) | W6.2 closure: OpenAPI v2 websocket 集成 (~272 LoC) |
| DingTalk     | ✅ Stage 1 native | ✅ 完整 4 步(`open-dingtalk/dingtalk-stream-sdk-go` v0.9.1) | W6.2 closure: StreamCallback 长连接 (~245 LoC) |
| Email        | ✅ Stage 1 native | ✅ 完整 4 步(`emersion/go-imap` v1.2.1 + stdlib `net/smtp`) | W6.2 closure: IMAP IDLE + SMTP 发送 (~363 LoC) |

> **W5 反 W4.1 设计反转**(per 2026-07-13 Q1=B 拍板):SDK 端 bot/ 现已支持 8 平台(原 W4.1 仅 3 平台);5 平台 (Slack/Feishu/QQ/DingTalk/Email) 走 SDK 端 Stage 0 stub 替代原"⛔ 走服务端 adapter"。Stage 1 路径(per M11 W5-W6)将替换 stub 为 native SDK integration。W7 之后 wau-channel 8 平台 adapter 全部完整(per W7 2026-07-07 SDK 接通)。
>
> **W6.2 closure (2026-07-09)**:5 平台 (Slack/Feishu/QQ/DingTalk/Email) Stage 0 stub → Stage 1 native SDK integration 100% 收口(per M10 N1 + D13 + D78 + D80 公共契约)。

**使用范式**(4 SDK 一致,Go SDK 示例):

```go
// SDK 端(B 端开发者):通过 BotsService 公共契约操作 bot
client.Bots().Register(ctx, wau.RegisterBotRequest{
    TenantID:     "acme",
    Universe:     "default",
    PublicBotID:  "weather-bot",
})

// 平台通信端:平台 SDK 自动选择 — 通过 wau-channel 服务端 adapter 调用
// SDK 不需要直接 import slack/feishu/... — 走 wau-channel HTTP API
```

> **本节由 W4.1 README 标准化自动 append,2026-07-13**。D60 additive:0 改 README 老内容。
>
> 关联:`WAU-develop/develop-log/kernel/v1.0.0/stage2/2026-07-13-PROGRESS-W4-launch.md`

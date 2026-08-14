# wau-go-sdk

> WAU 网络官方 Go SDK —— 五行代码即可连接 `wau-core-kernel` 智能调度内核及完整的 WAU 服务网格。

[English](README.md) | [中文](README.zh-CN.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/wau/wau-go-sdk.svg)](https://pkg.go.dev/github.com/wau/wau-go-sdk)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-blue)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/wau/wau-go-sdk/actions/workflows/ci.yml/badge.svg)](https://github.com/wau/wau-go-sdk/actions)

## 核心特性

一个 `Client` 即可覆盖 WAU 内核的 8 个核心公共服务,同时附带 2 个独立协议网关 sub-client 与 1 个支持 8 个聊天平台的 Bot 抽象。

- **8 个核心服务** —— Agents、Tasks、Kernel、Intent、Handshake、Chat、Skills、L5(包管理器客户端),全部通过同一个 `wau.New(...)` 句柄调用。
- **内置弹性** —— 指数退避 + 抖动重试、按 Client 实例隔离的熔断器、强类型错误(`errors.Is(err, ErrNotFound)`)、HS256 JWT 鉴权,在 `doWithRetry` 里全部默认启用。
- **可插拔追踪** —— 实现 `Tracer` 接口即可对接 OpenTelemetry / OpenTracing / 自研后端;不传时使用零依赖的 no-op 实现。
- **2 个协议网关 sub-client** —— `mcpclient`(JSON-RPC 2.0 over HTTP,7 个 MCP sync tool + 3 个 W4/W5 占位)、`ucpclient`(11 个 commerce tool,Stripe-ready)走独立 transport。
- **8 个 Bot 平台** —— Telegram、Discord、Webhook、Slack、Feishu、QQ、DingTalk、Email 都在 `bot/` 子目录下,每个平台都配对 `wau-channel` 服务端 adapter。
- **OAuth + PKCE** —— 生产可用的 `OAuthClient`、`RefreshableTokenStore`、`PKCEClient`,覆盖 client-credentials 与 authorization-code 两条路径。
- **Skill 市场** —— 列表、上架、加载符合 `agentskills.io` 规范的 skill 包(`Skills().List / Get / Publish / LoadForUser`)。
- **测试覆盖门槛** —— `go test -race -coverprofile` 在 CI(GO 1.25 / Ubuntu)上强制覆盖率 ≥ 80%。

## 安装

```bash
go get github.com/wau/wau-go-sdk@v1.3.4
```

前置:**Go 1.25+**(在 Go 1.23 / 1.24 上也能编译)。模块路径为 `github.com/wau/wau-go-sdk`。

## 快速上手

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    wau "github.com/wau/wau-go-sdk"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    client, err := wau.New("http://localhost:18400", wau.WithTimeout(5*time.Second))
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    info, err := client.Kernel().Info(ctx)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("kernel %s — uptime %ds\n", info.Version, info.Uptime)
}
```

运行:

```bash
go run main.go
# kernel v1.x.y — uptime 42s
```

## Public API

根包导出一个 `Client`,提供 8 个 service getter,全部走同一条 `doWithRetry` 链路(鉴权 → 熔断 → 重试 → HTTP)。

| 服务              | Getter                  | 覆盖范围                                                                                          |
|-------------------|-------------------------|---------------------------------------------------------------------------------------------------|
| `AgentsService`   | `client.Agents()`       | list / get / register / deregister / score / heartbeat / report-load(HTTP `/registry/agents`)       |
| `TasksService`    | `client.Tasks()`        | submit / simulate / get(HTTP `/registry/tasks/submit`)                                            |
| `KernelService`   | `client.Kernel()`       | `Info()` 与 `Health()`(HTTP `/kernel/info`)                                                       |
| `IntentService`   | `client.Intent()`       | gRPC 存根(M3.1 实装,目前返 `ErrNotImplemented`)                                                   |
| `ChatService`     | `client.Chat()`         | 对 `wau-edge` 发 OpenAI 兼容的 chat completion + SSE 流式输出                                       |
| `HandshakeService`| `client.Handshake()`    | A2A / AFP 会话协商                                                                                 |
| `SkillsService`   | `client.Skills()`       | list / get / publish / load user skill(multipart 上传到 `wau-registry`)                            |
| `L5Service`       | `client.L5()`           | install / uninstall / update / search / login —— WAU 包管理器客户端                                |

`Client` 上还暴露了辅助方法:`BaseURL()`、`CircuitState()`、`Close()`。Transport 选项:`WithTimeout`、`WithAuth`、`WithRetry`、`WithCircuit`、`WithLogger`、`WithTracer`。

### Sub-client

```go
// MCP —— JSON-RPC 2.0 over HTTP,endpoint: POST {baseURL}/mcp
mcp, _ := mcpclient.NewClient("https://kernel.example.com",
    mcpclient.WithBearerToken(jwt),
)
card, _ := mcp.ParseAgentCard(ctx, []byte(`{"name":"Fox"}`))

// UCP —— JSON-RPC 2.0 over HTTP,endpoint: POST {baseURL}/ucp
ucp, _ := ucpclient.NewClient("https://kernel.example.com",
    ucpclient.WithBearerToken(oauthJWT),
)
cart, _ := ucp.AddToCart(ctx, "prod-123", 2)
```

### Bot 平台

```go
import "github.com/wau/wau-go-sdk/bot/telegram"

bot := telegram.New(telegram.Config{
    Token:    os.Getenv("TELEGRAM_TOKEN"),
    TenantID: "acme",
})
if err := bot.Start(); err != nil {
    log.Fatal(err)
}
```

Discord、Webhook、Slack、Feishu、QQ、DingTalk、Email 都同款 —— 每个平台都在 `bot/<name>/` 下,统一实现 `Bot` 接口。

## 示例程序

[`examples/`](examples/) 目录收录 9 个可运行程序:

| 目录                       | 演示内容                                                                |
|----------------------------|-------------------------------------------------------------------------|
| `examples/list_agents/`    | 翻页拉 `Agents().List`                                                  |
| `examples/submit_task/`    | 提交任务并轮询 `Tasks().Get`                                            |
| `examples/heartbeat_loop/` | 60 秒一次的 agent 心跳循环                                              |
| `examples/five_scenarios/` | 5 个标准场景契约测试(clinical / france / pain / sales / rare-disease)   |
| `examples/chat_stream/`    | 消费 SSE 流式 chat completion                                           |
| `examples/multi_llm_e2e/`  | 跨多家 LLM 后端的 e2e 测试                                              |
| `examples/bot_telegram/`   | Telegram bot 接入 kernel tenant                                         |
| `examples/bot_discord/`    | Discord bot 接入 kernel tenant                                          |
| `examples/bot_webhook/`    | 通用 webhook bot                                                        |

进入任一目录运行:

```bash
cd examples/list_agents
go run .
```

## 文档

| 文档                                              | 用途                                                                  |
|---------------------------------------------------|-----------------------------------------------------------------------|
| [ARCHITECTURE.md](ARCHITECTURE.md)                | 模块布局、transport 链路、bot / MCP / UCP 边界                         |
| [API.md](API.md)                                  | 按 service 组织的 API 参考(8 个 service + bot 子包)                   |
| [FAQ.md](FAQ.md)                                  | 鉴权、重试、熔断、gRPC 存根、bot 桥接等常见问题                        |
| [QUICKSTART.md](QUICKSTART.md)                    | 15 分钟端到端走查                                                      |
| [DEPLOY.md](DEPLOY.md)                            | 版本管理与 Go module 指南                                              |
| [RELEASING.md](RELEASING.md)                      | 发版流程                                                               |
| [CHANGELOG.md](CHANGELOG.md)                      | 版本化的发版历史                                                       |
| [`docs/quickstart.md`](docs/quickstart.md)        | 5 分钟上手(legacy `docs/` 路径)                                       |
| [`docs/api.md`](docs/api.md)                      | API 参考镜像                                                           |
| [`docs/auth.md`](docs/auth.md)                    | HS256 JWT 鉴权指南                                                     |
| [`docs/retry_circuit.md`](docs/retry_circuit.md)  | 重试 + 熔断器内部机制                                                  |

## 版本策略

本次发版为 **v1.3.4**。Go SDK 遵循 [WAU 19 仓 lockstep 版本政策](https://github.com/wau/wau-go-sdk/blob/main/CONTRIBUTING.md#version-sync-policy):任一兄弟 SDK(Python / TypeScript / Rust / Java 或 core kernel)发 `v1.3.X`,本仓必须在同一窗口发 `v1.3.X`。SDK 严格遵守 [SemVer 2.0.0](https://semver.org/);public API 自 v1.0.0(2026-07-25)起稳定,breaking change 只走 MAJOR。已废弃的 API 至少保留一个 minor 版本,并在编译期提示迁移方向。

## 贡献

欢迎 PR。请先读 [CONTRIBUTING.md](CONTRIBUTING.md) —— 里面写了开发环境搭建、测试命令、lint 政策、PR 流程,以及 WAU 19 仓的版本同步规则。

## 协议

MIT —— 见 [LICENSE](LICENSE)。Copyright © 2026 youhaoxi.

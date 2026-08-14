# wau-go-sdk

> Official Go SDK for the WAU network — connect to `wau-core-kernel` and the full WAU service mesh in five lines of Go.

[English](README.md) | [中文](README.zh-CN.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/wau/wau-go-sdk.svg)](https://pkg.go.dev/github.com/wau/wau-go-sdk)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-blue)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/wau/wau-go-sdk/actions/workflows/ci.yml/badge.svg)](https://github.com/wau/wau-go-sdk/actions)

## Features

A single `Client` covers the eight public services of the WAU kernel plus two protocol-gateway sub-clients and a Bot platform abstraction for eight chat networks.

- **Eight core services** — Agents, Tasks, Kernel, Intent, Handshake, Chat, Skills, and L5 (the package-manager client) are all reachable through one `wau.New(...)` handle.
- **Built-in resilience** — exponential backoff with jitter, per-Client circuit breaker, typed errors (`errors.Is(err, ErrNotFound)`), and HS256 JWT auth are wired into every request by `doWithRetry`.
- **Pluggable tracing** — implement the `Tracer` interface to bridge OpenTelemetry / OpenTracing / your own backend; the default is a zero-dependency no-op.
- **Two protocol-gateway sub-clients** — `mcpclient` (JSON-RPC 2.0 over HTTP, 7 sync MCP tools + 3 deferred) and `ucpclient` (11 commerce tools, Stripe-ready) talk to the kernel through independent transports.
- **Eight Bot platforms** — Telegram, Discord, Webhook, Slack, Feishu, QQ, DingTalk, and Email are first-class citizens under `bot/`; each pair has a matching server-side adapter in `wau-channel`.
- **OAuth + PKCE** — production-ready `OAuthClient`, `RefreshableTokenStore`, and `PKCEClient` for client-credentials and authorization-code flows.
- **Skills marketplace** — list, publish, and load `agentskills.io`-compliant skill bundles (`Skills().List / Get / Publish / LoadForUser`).
- **Test coverage gate** — `go test -race -coverprofile` keeps total coverage at or above 80% in CI on Go 1.25 / Ubuntu.

## Installation

```bash
go get github.com/wau/wau-go-sdk@v1.3.4
```

Requirements: **Go 1.25+** (the SDK also builds on Go 1.23 / 1.24). The module path is `github.com/wau/wau-go-sdk`.

## Quickstart

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

Run it:

```bash
go run main.go
# kernel v1.x.y — uptime 42s
```

## Public API

The root package exposes one `Client` with eight service getters, all backed by the same `doWithRetry` chain (auth → circuit breaker → retry → HTTP).

| Service            | Getter                  | What it covers                                                                                       |
|--------------------|-------------------------|------------------------------------------------------------------------------------------------------|
| `AgentsService`    | `client.Agents()`       | list / get / register / deregister / score / heartbeat / report-load (HTTP `/registry/agents`)        |
| `TasksService`     | `client.Tasks()`        | submit / simulate / get (HTTP `/registry/tasks/submit`)                                              |
| `KernelService`    | `client.Kernel()`       | `Info()` and `Health()` (HTTP `/kernel/info`)                                                        |
| `IntentService`    | `client.Intent()`       | gRPC stub for intent parsing (M3.1 wiring; returns `ErrNotImplemented` today)                        |
| `ChatService`      | `client.Chat()`         | OpenAI-compatible chat completions + SSE streaming against `wau-edge`                                |
| `HandshakeService` | `client.Handshake()`    | A2A / AFP session negotiation                                                                         |
| `SkillsService`    | `client.Skills()`       | list / get / publish / load user skills (multipart upload to `wau-registry`)                         |
| `L5Service`        | `client.L5()`           | install / uninstall / update / search / login — the WAU package-manager client                       |

Helpers exposed on `Client`: `BaseURL()`, `CircuitState()`, `Close()`. Transport options: `WithTimeout`, `WithAuth`, `WithRetry`, `WithCircuit`, `WithLogger`, `WithTracer`.

### Sub-clients

```go
// MCP — JSON-RPC 2.0 over HTTP, endpoint POST {baseURL}/mcp
mcp, _ := mcpclient.NewClient("https://kernel.example.com",
    mcpclient.WithBearerToken(jwt),
)
card, _ := mcp.ParseAgentCard(ctx, []byte(`{"name":"Fox"}`))

// UCP — JSON-RPC 2.0 over HTTP, endpoint POST {baseURL}/ucp
ucp, _ := ucpclient.NewClient("https://kernel.example.com",
    ucpclient.WithBearerToken(oauthJWT),
)
cart, _ := ucp.AddToCart(ctx, "prod-123", 2)
```

### Bot platforms

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

Same shape for Discord, Webhook, Slack, Feishu, QQ, DingTalk, and Email — every platform lives under `bot/<name>/` and uses a single `Bot` interface.

## Examples

The [`examples/`](examples/) directory ships nine runnable programs:

| Directory                  | What it shows                                                                |
|----------------------------|------------------------------------------------------------------------------|
| `examples/list_agents/`    | page through `Agents().List`                                                 |
| `examples/submit_task/`    | submit a task and poll `Tasks().Get`                                         |
| `examples/heartbeat_loop/` | 60-second agent heartbeat loop                                               |
| `examples/five_scenarios/` | the canonical five scenario contract tests (clinical / france / pain / sales / rare-disease) |
| `examples/chat_stream/`    | consume SSE streaming chat completions                                       |
| `examples/multi_llm_e2e/`  | end-to-end test against multiple LLM backends                                |
| `examples/bot_telegram/`   | Telegram bot wired to a kernel tenant                                        |
| `examples/bot_discord/`    | Discord bot wired to a kernel tenant                                         |
| `examples/bot_webhook/`    | generic webhook bot                                                          |

Run any of them with:

```bash
cd examples/list_agents
go run .
```

## Documentation

| Doc                                            | Purpose                                                                  |
|------------------------------------------------|--------------------------------------------------------------------------|
| [ARCHITECTURE.md](ARCHITECTURE.md)              | Module layout, transport chain, bot / MCP / UCP boundaries               |
| [API.md](API.md)                                | Service-by-service API reference (8 services + bot sub-package)          |
| [FAQ.md](FAQ.md)                                | Common questions on auth, retry, circuit, gRPC stub, bot bridges         |
| [QUICKSTART.md](QUICKSTART.md)                 | 15-minute end-to-end walkthrough                                          |
| [DEPLOY.md](DEPLOY.md)                          | Versioning and Go module guidance                                         |
| [RELEASING.md](RELEASING.md)                    | Release process                                                           |
| [CHANGELOG.md](CHANGELOG.md)                    | Versioned release history                                                 |
| [`docs/quickstart.md`](docs/quickstart.md)      | 5-minute walkthrough (legacy `docs/` location)                           |
| [`docs/api.md`](docs/api.md)                    | API reference mirror                                                      |
| [`docs/auth.md`](docs/auth.md)                  | HS256 JWT auth guide                                                      |
| [`docs/retry_circuit.md`](docs/retry_circuit.md)| Retry + circuit-breaker internals                                         |

## Versioning

This release is **v1.3.4**. The Go SDK follows the [WAU 19-repo lockstep versioning policy](https://github.com/wau/wau-go-sdk/blob/main/CONTRIBUTING.md#version-sync-policy): when any sibling SDK (Python, TypeScript, Rust, Java, or the core kernel) ships `v1.3.X`, this repository must release `v1.3.X` in the same window. The SDK uses [SemVer 2.0.0](https://semver.org/); the public API is stable since v1.0.0 (2026-07-25), and breaking changes flow only through MAJOR bumps. Deprecated APIs are supported for at least one minor release with a compile-time migration hint.

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) for the development setup, test command, lint policy, pull-request workflow, and the WAU 19-repo version sync rule.

## License

MIT — see [LICENSE](LICENSE). Copyright © 2026 youhaoxi.

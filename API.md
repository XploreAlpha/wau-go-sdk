# wau-go-sdk API 参考

> **版本**:v1.1.0(v0.9.0 "Acorn" Stage 3.2 完整化,2026-07-01)
> **模块路径**:`github.com/wau/wau-go-sdk`
> **API 数量**:11 HTTP 端点 × 6 服务(Agents / Tasks / Kernel / Intent / Handshake / Chat)+ Bot 子包(per D13)
> **配套教程**:`docs/quickstart.md` 5 分钟入门,`docs/auth.md` HS256 鉴权,`docs/retry_circuit.md` 重试/熔断

---

## 目录

1. [安装](#1-安装)
2. [Client 初始化](#2-client-初始化)
3. [核心 API](#3-核心-api)
   - [3.1 Auth — Signer / AuthConfig / Role](#31-auth--signer--authconfig--role)
   - [3.2 KernelService](#32-kernelservice)
   - [3.3 AgentsService](#33-agentsservice)
   - [3.4 TasksService](#34-tasksservice)
   - [3.5 HandshakeService](#35-handshakeservice)
   - [3.6 IntentService(M3.1 stub)](#36-intentservice-m31-stub)
   - [3.7 ChatService ⭐(Stage 3.1 2xx 验证)](#37-chatservice-stage-3-1-2xx-验证)
   - [3.8 Retry / Circuit 状态查询](#38-retry--circuit-状态查询)
4. [Bot 子包(per D13)](#4-bot-子包per-d13)
5. [配置项](#5-配置项)
6. [类型定义](#6-类型定义)
7. [错误码](#7-错误码)
8. [版本与变更](#8-版本与变更)

---

## 1. 安装

```bash
# 1.1 标准 go get
go get github.com/wau/wau-go-sdk@v1.1.0

# 1.2 go.mod 锁版本
require github.com/wau/wau-go-sdk v1.1.0

# 1.3 依赖(自动拉取)
#   - github.com/golang-jwt/jwt/v5   (HS256 鉴权)
#   - github.com/google/uuid         (jti 防重放)
#   - github.com/go-telegram-bot-api/telegram-bot-api/v5 (Bot 集成)
```

**前置依赖**:
- Go ≥ 1.23(用了 Go 1.23 迭代器,per `c.Agents().Iter()`)
- 目标 WAU 服务:wau-core-kernel(默认 `:18400`)+ wau-edge(默认 `:18402` Chat)

---

## 2. Client 初始化

### 2.1 `wau.New(baseURL, opts...)`

**创建 SDK 客户端**。

| 参数 | 类型 | 说明 |
|---|---|---|
| `baseURL` | `string` | wau-core-kernel HTTP 地址,例如 `http://localhost:18400` |
| `opts` | `...Option` | 0+ 个 `WithXxx()` 配置(Functional Options 模式) |

**返回**:
- `*Client`(并发安全,immutable)
- `error`:配置错误(例如 `WithAuth` 缺 `SharedSecret`)

**示例**:
```go
package main

import (
    "context"
    "fmt"
    "time"

    wau "github.com/wau/wau-go-sdk"
)

func main() {
    ctx := context.Background()

    // 最简用法
    c, err := wau.New("http://localhost:18400")
    if err != nil {
        panic(err)
    }
    defer c.Close()

    // 完整配置(超时 + 重试 + 熔断 + 鉴权)
    c, err = wau.New("http://localhost:18400",
        wau.WithTimeout(10*time.Second),
        wau.WithRetry(wau.DefaultRetryConfig()),
        wau.WithCircuit(wau.DefaultCircuitConfig()),
        wau.WithAuth(wau.AuthConfig{
            Role:         wau.RoleExternalAgent,
            AgentName:    "my-agent",
            TenantID:     "tenant-A",  // 必填(per Stage 3.1 #1 修复)
            SharedSecret: []byte(os.Getenv("WAU_EDGE_JWT_SECRET")),
        }),
    )
    if err != nil {
        panic(err)
    }

    fmt.Println("BaseURL:", c.BaseURL())
}
```

### 2.2 完整 Option 列表

| Option | 默认 | 用途 |
|---|---|---|
| `WithHTTPClient(hc)` | `http.Client{Timeout: 30s}` | 注入自定义 HTTP client(代理 / 测试) |
| `WithTimeout(d)` | `30s` | 单次请求超时 |
| `WithRetry(cfg)` | `RetryConfig{MaxRetries: 3, InitialBackoff: 200ms, MaxBackoff: 5s}` | 重试策略 |
| `WithRetryNo()` | — | 禁用重试(`MaxRetries=0`) |
| `WithCircuit(cfg)` | `CircuitConfig{FailureThreshold: 5, OpenTimeout: 30s}` | 熔断策略 |
| `WithCircuitDisabled()` | — | 禁用熔断(测试用) |
| `WithAuth(cfg)` | —(可选,没配 = 匿名) | HS256 JWT 鉴权 |
| `WithLogger(l)` | `slog.Default()` | 自定义 slog logger |
| `WithUserAgent(ua)` | `wau-go-sdk/0.6.0-preview.1` | User-Agent 头 |
| `WithTracer(t)` | `noopTracer{}` | OTel-compatible tracer(v0.7.0 W1) |

### 2.3 `(*Client).Close() error`

释放资源。**当前 v0.9.0 alpha 是 no-op**(gRPC client M3.1 才需要),后续 gRPC client 实装后会自动关闭。

### 2.4 `(*Client).BaseURL() string`

返回 base URL(debug / metrics 用)。

### 2.5 `(*Client).CircuitState() string`

返回当前熔断状态:`"closed"` / `"open"` / `"half-open"`。

---

## 3. 核心 API

### 3.1 Auth — Signer / AuthConfig / Role

> per Stage 3.1 #1+#2 修复(2026-07-01):wau-edge Claims 必填 `tenant_id`,SDK 必须签。

#### `Role` enum

| 值 | 用途 |
|---|---|
| `RoleKernelCore` | 内核内部调用(`kernel_core`) |
| `RoleTrustedAgent` | 受信 agent(`trusted_agent`) |
| `RoleExternalAgent` | **默认**,外部 agent(`external_agent`) |

#### `AuthConfig` struct

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `Role` | `Role` | 否 | 默认 `RoleExternalAgent` |
| `AgentName` | `string` | **是** | 标识当前 agent,放入 JWT `agent` claim |
| `TenantID` | `string` | **是** | **必填**(per Stage 3.1 #1 修复,wau-edge Claims 校验),空字符串 → `newSigner` 返错 |
| `Subject` | `string` | 否 | JWT `sub` claim,空 → 用 `AgentName` 兜底 |
| `SharedSecret` | `[]byte` | **是** | HS256 密钥(从环境变量读,**不写死**) |

#### JWT Claims(7 字段,per auth.go:65-74)

```json
{
  "agent":     "my-agent",
  "role":      "external_agent",
  "sub":       "my-agent",
  "tenant_id": "tenant-A",
  "iat":       1751376000,
  "exp":       1751376300,
  "jti":       "550e8400-e29b-41d4-a716-446655440000"
}
```

- `exp`:5 分钟(短,每次请求新签,减重放窗口)
- `jti`:UUID v4 防重放

#### 完整 Auth 示例

```go
import (
    "os"
    wau "github.com/wau/wau-go-sdk"
)

secret := os.Getenv("WAU_EDGE_JWT_SECRET") // 从环境变量读
if secret == "" {
    panic("WAU_EDGE_JWT_SECRET not set")
}

c, err := wau.New("http://localhost:18400",
    wau.WithAuth(wau.AuthConfig{
        Role:         wau.RoleTrustedAgent,
        AgentName:    "Benny",
        TenantID:     "tenant-A", // 必填
        Subject:      "Benny-session-1", // 可选
        SharedSecret: []byte(secret),
    }),
)
```

**详细教程**:[docs/auth.md](./docs/auth.md)

### 3.2 KernelService

#### `c.Kernel().Info(ctx) (*KernelInfo, error)`

`GET /kernel/info` — 返回 kernel 元信息(version / uptime / agent count)。

#### `c.Kernel().Health(ctx) (*HealthResponse, error)`

`GET /health` — 检查 kernel 健康(redis 连通性 / version / uptime)。

### 3.3 AgentsService

#### `c.Agents().Health(ctx) (*HealthResponse, error)`

`GET /health`(暴露在 Agents 上方便调用)。

#### `c.Agents().List(ctx, opts) (*AgentListResponse, error)`

`GET /registry/agents?page=...&pageSize=...&skill=...&status=...&search=...`

**`PageOptions` 字段**:

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `Page` | `int` | 1 | 1-based 页码 |
| `PageSize` | `int` | 10 | 最大 100 |
| `Skill` | `string` | — | 可选技能过滤 |
| `Status` | `string` | — | 可选状态过滤 |
| `Search` | `string` | — | 可选模糊匹配 |

#### `c.Agents().Iter(ctx, opts) func(yield func(Agent, error) bool)`

Go 1.23+ **迭代器**,遍历所有页:

```go
for a, err := range c.Agents().Iter(ctx, wau.PageOptions{Skill: "clinical"}) {
    if err != nil {
        log.Printf("iter err: %v", err)
        continue
    }
    fmt.Println(a.Name, "trust=", a.Trust)
}
```

#### `c.Agents().Get(ctx, name) (*AgentStatus, error)`

`GET /registry/agents/{name}/status` — 综合状态(load + trust + circuit)。

#### `c.Agents().Score(ctx, name) (*AgentScore, error)`

`GET /registry/agents/{name}/score` — 5 维评分(总分 + trust + skill + health + load)。

#### `c.Agents().Register(ctx, req) error`

`POST /registry/agents/register` — 注册新 agent(RBAC: `trusted_agent` / `kernel_core`)。

```go
err := c.Agents().Register(ctx, wau.AgentRegisterRequest{
    Name:        "my-agent",
    URL:         "http://my-agent:18800",
    Description: "Medical triage bot",
    Skills:      []string{"clinical-decision-support"},
    Universes:   []string{"medical"},
})
```

#### `c.Agents().Deregister(ctx, name) error`

`DELETE /registry/agents/{name}` — 注销 agent。

#### `c.Agents().Heartbeat(ctx, agentID) error`

`POST /registry/agents/heartbeat` — agent 主动心跳上报(60s 一次)。

#### `c.Agents().ReportLoad(ctx, agentID, load) error`

`POST /heartbeat/load` — 上报运行时负载(ActiveTasks / MaxCapacity / CPU / Memory)。

### 3.4 TasksService

#### `c.Tasks().Submit(ctx, req) (*SubmitResponse, error)`

`POST /registry/tasks/submit` — L4 真发 A2A。

```go
resp, err := c.Tasks().Submit(ctx, wau.SubmitRequest{
    Prompt:    "What is the capital of France?",
    TimeoutMs: 30000,
})
// resp.SelectedAgent, resp.Score, resp.Response, resp.A2ACallMs
```

**注意**:`SubmitRequest` 只有 2 字段(`Prompt` + `TimeoutMs`),wau-cli 旧 DTO(`Message` / `SourcePeer`)已废弃,SDK 以 kernel 真相源为准。

#### `c.Tasks().Simulate(ctx, req) (*DecisionInfo, error)`

`POST /registry/tasks/simulate` — L3 决策(不真发)。

#### `c.Tasks().Get(ctx, taskID) (*Task, error)`

`GET /registry/tasks/{taskID}` — 查询任务详情。

### 3.5 HandshakeService

> per v0.8.0 M5-1 B.1 实装,9 个握手 sentinel error。

#### `c.Handshake().CreateSession(ctx, req) (*HandshakeResponse, error)`

`POST /v0.8.0/handshake/sessions`

| 字段 | 必填 | 说明 |
|---|---|---|
| `TenantID` | **是** | 租户 ID |
| `ClientID` | 否 | 不填时自动用 SDK `user_agent`(如 `wau-go-sdk/1.1.0`) |
| `AgentID` | **是** | agent 标识(如 `Benny`) |
| `Protocol` | 否 | 默认 `a2a` |
| `Universe` | 否 | 业务分组 |

**响应 6 字段**:`SessionID` / `DirectEndpoint` / `Protocol` / `ExpiresAt` / `TTLSeconds` / `Reused`。

#### `c.Handshake().GetSession(ctx, sessionID, tenantID) (*HandshakeSessionDetail, error)`

`GET /v0.8.0/handshake/sessions/{session_id}?tenant_id=xxx` — 11 字段详情(含 `TrustScore` / `ReuseCount`)。

#### `c.Handshake().GetStats(ctx) (*HandshakeStats, error)`

`GET /admin/handshake/stats` — hit rate 监控数据(`TotalSessions` / `ReuseHitRate` / `PerTenant`)。

#### 9 个握手 sentinel error

```go
import "errors"

resp, err := c.Handshake().CreateSession(ctx, req)
switch {
case errors.Is(err, wau.ErrHandshakeAgentNotFound):
    // 404 AGENT_NOT_FOUND
case errors.Is(err, wau.ErrHandshakeTenantMismatch):
    // 403 TENANT_MISMATCH
case errors.Is(err, wau.ErrHandshakeInsufficientTrust):
    // 403 INSUFFICIENT_TRUST
case errors.Is(err, wau.ErrHandshakeRateLimited):
    // 429 RATE_LIMITED
case errors.Is(err, wau.ErrHandshakeProtocolNotSupported):
    // 400 PROTOCOL_NOT_SUPPORTED
case errors.Is(err, wau.ErrHandshakeSessionNotFound):
    // 404 SESSION_NOT_FOUND
case errors.Is(err, wau.ErrHandshakeAgentNoEndpoint):
    // 404 AGENT_NO_ENDPOINT
case errors.Is(err, wau.ErrHandshakeInvalidProtocol):
    // 400 INVALID_PROTOCOL
case errors.Is(err, wau.ErrHandshakeInvalidRequest):
    // 400 INVALID_REQUEST
}
```

### 3.6 IntentService(M3.1 stub)

> v0.9.0 P2 阶段 stub,所有方法返 `wau.ErrNotImplemented`:

- `(*IntentService).Recommend(ctx, prompt, topK) (any, error)`
- `(*IntentService).ParseIntent(ctx, text) (any, error)`
- `(*IntentService).ListAgents(ctx, onlineOnly) (any, error)`
- `(*IntentService).HealthCheck(ctx) (any, error)`

**留给 M3.1 实装,本期(07-01)不实现**。

### 3.7 ChatService ⭐(Stage 3.1 2xx 验证)

> per Stage 3.1 #4 实装,4 SDK 全 2xx 验证(Go/Python/TS/Rust)。

#### `c.Chat().Completions(ctx, req) (*ChatCompletionResponse, error)`

`POST /v1/chat/completions` — wau-edge OpenAI 兼容层主入口。

**`ChatCompletionRequest` 字段**(OpenAI spec 1:1 对齐):

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `Model` | `string` | **是** | 模型名(如 `gpt-4o-mini` / `claude-haiku`),空 → 返错 |
| `Messages` | `[]ChatMessage` | **是** | ≥ 1 条 user 消息 |
| `Stream` | `bool` | 否 | 雏形期只支持 `false`(M3 §3.7 续) |
| `Universe` | `string` | 否 | WAU 扩展,业务分组,默认 `default` |
| `Metadata` | `map[string]string` | 否 | WAU 扩展,自由 metadata 通道 |
| `Temperature` | `*float64` | 否 | OpenAI 标准 |
| `MaxTokens` | `int` | 否 | OpenAI 标准 |

**真实 2xx 验证示例**(per Stage 3.1 #4,2026-06-30):

```go
package main

import (
    "context"
    "fmt"
    "os"

    wau "github.com/wau/wau-go-sdk"
)

func main() {
    ctx := context.Background()

    // 注意:wau-edge 默认端口 18402(不是 wau-core 的 18400)
    c, err := wau.New("http://localhost:18402",
        wau.WithAuth(wau.AuthConfig{
            Role:         wau.RoleExternalAgent,
            AgentName:    "chat-bot",
            TenantID:     "acme", // 必填
            SharedSecret: []byte(os.Getenv("WAU_EDGE_JWT_SECRET")),
        }),
    )
    if err != nil {
        panic(err)
    }
    defer c.Close()

    resp, err := c.Chat().Completions(ctx, wau.ChatCompletionRequest{
        Model: "wau-default", // Stage 1 MockModels 唯一接受
        Messages: []wau.ChatMessage{
            {Role: "user", Content: "Hello, who are you?"},
        },
        Universe: "prod",
    })
    if err != nil {
        panic(err)
    }

    fmt.Printf("chatcmpl ID: %s\n", resp.ID)
    // 实际跑出: chatcmpl-787dcac6
    fmt.Printf("Content: %s\n", resp.Choices[0].Message.Content)
    fmt.Printf("Tokens: %d (prompt=%d completion=%d)\n",
        resp.Usage.TotalTokens, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
    // 实际跑出: 11 tokens
}
```

**实测数据**(per Stage 3.1 #4 sdk-go-e2e):
- `chatcmpl ID`:`chatcmpl-787dcac6`
- `Tokens`:11(prompt + completion)
- 详见:`WAU-develop/develop-log/kernel/v0.9.0/stage3/2026-06-30-PROGRESS-M5-#4-sdk-go.md`

#### `c.Chat().CompletionsRaw(ctx, req) ([]byte, error)`

`POST /v1/chat/completions` 返回 **raw bytes**(给流式 / 字节级测试用)。

大多数情况用 `Completions` 即可;只在需要保留 server 原始响应字节时用这个。**雏形期 M3 §3.7 streaming 不实现,但 raw 接口预留**。

#### `ChatCompletionResponse` 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `ID` | `string` | chatcmpl-* ID |
| `Object` | `string` | 固定 `chat.completion` |
| `Created` | `int64` | Unix timestamp |
| `Model` | `string` | 实际使用的模型 |
| `Choices` | `[]ChatChoice` | 候选响应 |
| `Usage` | `ChatUsage` | token 用量 |
| `Reason` | `string` | WAU 扩展,wau-llm-router 决策原因(audit 用) |

### 3.8 Retry / Circuit 状态查询

#### `RetryConfig` 默认值

```go
wau.DefaultRetryConfig() // = {
    MaxRetries:     3,
    InitialBackoff: 200ms,
    MaxBackoff:     5s,
    Jitter:         0.2,
    RetryOn:        []int{500, 502, 503, 504, 429}, // 5xx 全部 + 429
}
```

策略:3 次重试总耗时 ~3.5s,**只对幂等请求自动重试**;非幂等 POST 默认不重试(给 opts override 入口)。

#### `CircuitConfig` 默认值

```go
wau.DefaultCircuitConfig() // = {
    FailureThreshold: 5,
    OpenTimeout:      30s,
    HalfOpenMax:      1,  // M3 W5.4 实装
    Enabled:          true,
}
```

#### `c.CircuitState() string`

返回当前熔断状态。

**详细教程**:[docs/retry_circuit.md](./docs/retry_circuit.md)

---

## 4. Bot 子包(per D13)

> 4 SDK 公共 `Bot interface`,所有平台 SDK 必须实现同样的方法签名。

### 4.1 `Bot` interface(per [bot/common/bot.go](bot/common/bot.go))

```go
type Bot interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    OnMessage(handler func(IncomingMessage) OutgoingMessage) Bot
    WithTenant(tenantID string) Bot
    WithUniverse(universe string) Bot
}
```

### 4.2 当前已实现 platform(per v0.9.0 alpha)

| Platform | 包路径 | 状态 |
|---|---|---|
| **Telegram** | `bot/telegram` | ✅ Stage 1 M1 §2.8 实装(2026-06-28) |
| **Discord** | `bot/discord` | ✅ Stage 1 M1 实装(Stage 3.1 #2 真联调推 v1.0.0) |
| **Webhook** | `bot/webhook` | ✅ Stage 1 M1 实装 |

**5+ 其他平台**(Slack / WhatsApp / 钉钉 / 飞书 / Email)推 **v1.0.0**(per D4)。

### 4.3 Telegram Bot 示例(5 行启动)

```go
package main

import (
    "context"
    "fmt"
    "os"

    botcommon "github.com/wau/wau-go-sdk/bot/common"
    bottelegram "github.com/wau/wau-go-sdk/bot/telegram"
)

func main() {
    ctx := context.Background()
    builder := botcommon.NewBuilder().
        WithTenant("acme").
        WithUniverse("prod")

    bot := bottelegram.New(os.Getenv("TELEGRAM_BOT_TOKEN"), builder)
    bot.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
        return botcommon.OutgoingMessage{Text: "Echo: " + in.Text}
    })

    if err := bot.Start(ctx); err != nil {
        panic(err)
    }
    fmt.Println("Telegram bot started")
}
```

### 4.4 Discord Bot

```go
import (
    botcommon "github.com/wau/wau-go-sdk/bot/common"
    botdiscord "github.com/wau/wau-go-sdk/bot/discord"
)

builder := botcommon.NewBuilder().WithTenant("acme").WithUniverse("prod")
bot := botdiscord.New(os.Getenv("DISCORD_BOT_TOKEN"), builder)
bot.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
    return botcommon.OutgoingMessage{Text: "Echo: " + in.Text}
})
bot.Start(ctx)
```

### 4.5 Webhook Bot

```go
import (
    botcommon "github.com/wau/wau-go-sdk/bot/common"
    botwebhook "github.com/wau/wau-go-sdk/bot/webhook"
)

builder := botcommon.NewBuilder().WithTenant("acme").WithUniverse("prod")
bot := botwebhook.New("https://my-webhook.example.com/wau", builder)
bot.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
    return botcommon.OutgoingMessage{Text: "Echo: " + in.Text}
})
bot.Start(ctx)
```

---

## 5. 配置项

### 5.1 ClientOptions vs YAML

**SDK 侧**:`Options` struct(per `options.go`)— 通过 Functional Options 模式注入。

**Server 侧**(wau-core-kernel `configs/kernel.yaml`):

```yaml
wau:
  client:
    timeout: 30s
    retry_count: 3
    circuit_failure_threshold: 5
    universe_default: prod
```

### 5.2 环境变量覆盖

| 环境变量 | 覆盖 | 默认 |
|---|---|---|
| `WAU_KERNEL_URL` | wau-core baseURL | `http://localhost:18400` |
| `WAU_EDGE_URL` | wau-edge baseURL(Chat 路径) | `http://localhost:18402` |
| `WAU_EDGE_JWT_SECRET` | `AuthConfig.SharedSecret` | —(必填,空 → 鉴权失败) |

### 5.3 关键配置项

| 项 | 类型 | 默认 | 用途 |
|---|---|---|---|
| `Timeout` | `time.Duration` | `30s` | 单次请求超时 |
| `Retry.MaxRetries` | `int` | `3` | 最大重试次数 |
| `Retry.InitialBackoff` | `time.Duration` | `200ms` | 初始退避 |
| `Retry.MaxBackoff` | `time.Duration` | `5s` | 最大退避 |
| `Retry.Jitter` | `float64` | `0.2` | 抖动比例 |
| `Circuit.FailureThreshold` | `uint32` | `5` | 熔断触发失败数 |
| `Circuit.OpenTimeout` | `time.Duration` | `30s` | 熔断 open 持续时间 |
| `Auth.Role` | `Role` | `RoleExternalAgent` | RBAC 角色 |
| `Auth.TenantID` | `string` | —(必填) | 租户 ID |

---

## 6. 类型定义

### 6.1 Chat / LLM(per `types.go:186-243`)

```go
type ChatMessage struct {
    Role    string `json:"role"`              // "system" / "user" / "assistant" / "tool"
    Content string `json:"content"`
    Name    string `json:"name,omitempty"`
}

type ChatCompletionRequest struct {
    Model       string             `json:"model"`
    Messages    []ChatMessage      `json:"messages"`
    Stream      bool               `json:"stream,omitempty"`
    Universe    string             `json:"universe,omitempty"`
    Metadata    map[string]string  `json:"metadata,omitempty"`
    Temperature *float64           `json:"temperature,omitempty"`
    MaxTokens   int                `json:"max_tokens,omitempty"`
}

type ChatChoice struct {
    Index        int         `json:"index"`
    Message      ChatMessage `json:"message"`
    FinishReason string      `json:"finish_reason"`
}

type ChatUsage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}

type ChatCompletionResponse struct {
    ID      string       `json:"id"`
    Object  string       `json:"object"`
    Created int64        `json:"created"`
    Model   string       `json:"model"`
    Choices []ChatChoice `json:"choices"`
    Usage   ChatUsage    `json:"usage"`
    Reason  string       `json:"reason,omitempty"`
}
```

### 6.2 Tasks / Handshake(per `types.go:111-166` + `handshake.go:48-89`)

```go
type SubmitRequest struct {
    Prompt    string `json:"prompt" binding:"required"`
    TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type SubmitResponse struct {
    TaskID        string       `json:"task_id"`
    AgentID       string       `json:"agent_id,omitempty"`
    AgentURL      string       `json:"agent_url,omitempty"`
    Score         float64      `json:"score,omitempty"`
    Dimensions    map[string]float64 `json:"dimensions,omitempty"`
    Decision      DecisionInfo `json:"decision"`
    Status        string       `json:"status"`
    SelectedAgent string       `json:"selected_agent,omitempty"`
    A2ACallMs     int          `json:"a2a_call_ms,omitempty"`
    Response      any          `json:"response,omitempty"`
    Error         string       `json:"error,omitempty"`
}

type DecisionInfo struct {
    SelectedAgent  string      `json:"selected_agent"`
    Score          float64     `json:"score"`
    DecisionTimeMs int         `json:"decision_time_ms"`
    Candidates     []Candidate `json:"candidates,omitempty"`
}

type HandshakeRequest struct {
    TenantID string `json:"tenant_id"`
    ClientID string `json:"client_id,omitempty"`
    AgentID  string `json:"agent_id"`
    Protocol string `json:"protocol,omitempty"`
    Universe string `json:"universe,omitempty"`
}

type HandshakeResponse struct {
    SessionID      string `json:"session_id"`
    DirectEndpoint string `json:"direct_endpoint"`
    Protocol       string `json:"protocol"`
    ExpiresAt      string `json:"expires_at"`
    TTLSeconds     int    `json:"ttl_seconds"`
    Reused         bool   `json:"reused"`
}
```

### 6.3 Agents(per `types.go:25-108`)

```go
type Agent struct {
    Name           string            `json:"name"`
    ID             string            `json:"id"`
    URL            string            `json:"url"`
    Description    string            `json:"description"`
    Skills         []string          `json:"skills"`
    Universes      []string          `json:"universes"`
    UniverseLabels map[string]string `json:"universe_labels,omitempty"` // v0.8.0 M3-2A
    Trust          float64           `json:"trust"`
    Status         string            `json:"status"`
    LastSeen       string            `json:"lastSeen"`
}

type AgentStatus struct {
    Name    string    `json:"name"`
    Status  string    `json:"status"`
    Trust   float64   `json:"trust"`
    Load    AgentLoad `json:"load"`
    Circuit string    `json:"circuit"`
}

type AgentScore struct {
    Name        string  `json:"name"`
    TotalScore  float64 `json:"totalScore"`
    TrustScore  float64 `json:"trustScore"`
    SkillMatch  float64 `json:"skillMatch"`
    HealthScore float64 `json:"healthScore"`
    LoadScore   float64 `json:"loadScore"`
}
```

---

## 7. 错误码

### 7.1 APIError 结构(per `errors.go:9-15`)

```go
type APIError struct {
    StatusCode int
    Code       string // optional, parsed from response body "code" field
    Message    string
    RequestID  string // optional X-Request-ID from response
    Body       []byte // raw response body for debugging
}
```

**使用示例**:
```go
resp, err := c.Chat().Completions(ctx, req)
if err != nil {
    var apiErr *wau.APIError
    if errors.As(err, &apiErr) {
        log.Printf("status=%d code=%s request_id=%s body=%s",
            apiErr.StatusCode, apiErr.Code, apiErr.RequestID, string(apiErr.Body))
    }
}
```

### 7.2 9 个 sentinel error(per `errors.go:36-46`)

| Sentinel | 状态码 | 触发 | 修复 |
|---|---|---|---|
| `wau.ErrNotFound` | 404 | 资源不存在 | 检查 ID / 路径 |
| `wau.ErrUnauthorized` | 401 | 鉴权失败 | 检查 JWT / `SharedSecret` / `tenant_id` |
| `wau.ErrForbidden` | 403 | RBAC 不足 | 检查 `Role` 权限 |
| `wau.ErrBadRequest` | 400 | 字段缺失 / 格式错 | 检查 DTO 字段 |
| `wau.ErrConflict` | 409 | 资源冲突 | 改名 / 换 ID |
| `wau.ErrInternal` | 500 | kernel 端 panic | 查 kernel 日志 |
| `wau.ErrCircuitOpen` | — | 熔断开 | 等 `OpenTimeout`(默认 30s) |
| `wau.ErrMaxRetries` | — | 重试耗尽(wraps last err) | 调 retry / circuit 配置 |
| `wau.ErrNotImplemented` | — | P2 stub | 等 M3.1 实装 |

### 7.3 9 个握手 sentinel error(per `handshake.go:155-165`)

| Sentinel | 状态码 | Code | 触发 |
|---|---|---|---|
| `wau.ErrHandshakeInsufficientTrust` | 403 | `INSUFFICIENT_TRUST` | trust score < 0.5 |
| `wau.ErrHandshakeAgentNotFound` | 404 | `AGENT_NOT_FOUND` | agent 不在 registry |
| `wau.ErrHandshakeTenantMismatch` | 403 | `TENANT_MISMATCH` | tenant 不持有此 session |
| `wau.ErrHandshakeRateLimited` | 429 | `RATE_LIMITED` | 100 req/min 超限 |
| `wau.ErrHandshakeProtocolNotSupported` | 400 | `PROTOCOL_NOT_SUPPORTED` | agent 不支持该 protocol |
| `wau.ErrHandshakeSessionNotFound` | 404 | `SESSION_NOT_FOUND` | session 不存在 / 过期 |
| `wau.ErrHandshakeAgentNoEndpoint` | 404 | `AGENT_NO_ENDPOINT` | agent 无 endpoint |
| `wau.ErrHandshakeInvalidProtocol` | 400 | `INVALID_PROTOCOL` | protocol 不在 ProtocolRegistry |
| `wau.ErrHandshakeInvalidRequest` | 400 | `INVALID_REQUEST` | 请求格式错 |

### 7.4 wau-edge 6 错误码(per chat.go:19-22,Stage 3.1 实测)

| 状态码 | Code | 触发 |
|---|---|---|
| 403 | `INSUFFICIENT_TRUST` | trust < 阈值 |
| 404 | `AGENT_NOT_FOUND` | agent 未注册 |
| 403 | `TENANT_MISMATCH` | tenant 不匹配 |
| 429 | `RATE_LIMITED` | 限流 |
| 400 | `PROTOCOL_NOT_SUPPORTED` | 不支持的 protocol |
| 400 | `INVALID_REQUEST` | 请求错 |
| 404 | `MODEL_NOT_FOUND` | wau-llm-router model 未注册 |

---

## 8. 版本与变更

### 8.1 当前版本

- **v1.1.0**(per Go module tag,2026-06-28)
- 内部 UA string:`wau-go-sdk/0.6.0-preview.1`(**已知 drift**,将在 v1.2.0 修复)
- 编译版本(per go.mod):Go 1.23+

### 8.2 升级指南

详见 [CHANGELOG.md](./CHANGELOG.md)。

**v0.8.0 → v1.1.0 关键变更**:
- ✅ `WithAuth` 必填 `TenantID`(per Stage 3.1 #1 修复,2026-07-01)— 老代码不填 `TenantID` → 401
- ✅ `ChatService.Completions()` 新增,OpenAI 兼容字段 1:1 对齐
- ✅ `HandshakeService` 9 sentinel error 实装
- ✅ `UniverseLabels` 字段新增(per v0.8.0 M3-2A,K8s-style)

### 8.3 v1.2.0 路线

- ❌ streaming(SSE)— v0.9.x gap,留 v1.2.0 实装
- ❌ IntentService 4 方法(M3.1 stub)— v1.2.0
- ❌ gRPC client(M3.1)— v1.2.0

### 8.4 历史里程碑

| 版本 | 日期 | 关键 |
|---|---|---|
| v0.6.0-preview.1 | 2026-06 | 雏形,从 wau-cli 抽取 |
| v0.7.0 | 2026-06 | 鉴权 / 熔断 / 重试 / typed errors |
| v0.8.0 | 2026-07-13 | M3-3 Embedding Cache / Bot 子包 / UniverseLabels |
| v1.0.0 | 2026-07 | 跟 wau-core-kernel v1.0.0 同步 |
| v1.1.0 | 2026-06-28 | ChatService / 5/5 字段对齐 / D13 Bot interface |
| v1.2.0 | (规划) | streaming / Intent / gRPC client |

---

## 链接

- [README.md](./README.md) — 入口
- [QUICKSTART.md](./QUICKSTART.md) — 15 分钟跑通 bot 场景
- [ARCHITECTURE.md](./ARCHITECTURE.md) — 模块 + 直连 chain
- [DEPLOY.md](./DEPLOY.md) — Release + 版本管理
- [CHANGELOG.md](./CHANGELOG.md) — 版本变更
- [docs/quickstart.md](./docs/quickstart.md) — 5 分钟 Hello World
- [docs/auth.md](./docs/auth.md) — HS256 + JWT 完整教程
- [docs/retry_circuit.md](./docs/retry_circuit.md) — Retry + Circuit 完整教程
- [docs/adr/0001-proto-sharing.md](./docs/adr/0001-proto-sharing.md) — Proto 共享
- [docs/adr/0002-sdk-stage-stratification.md](./docs/adr/0002-sdk-stage-stratification.md) — SDK 阶段分层
- [docs/adr/0003-circuit-integration.md](./docs/adr/0003-circuit-integration.md) — 熔断集成
- [docs/adr/0004-contract-golden-location.md](./docs/adr/0004-contract-golden-location.md) — Contract 真相源位置
- [examples/](./examples/) — 7 个 runnable example(含 bot_telegram / bot_discord / bot_webhook / heartbeat_loop / five_scenarios / list_agents / submit_task)

---

## 验收(per Stage 3.2 拍板)

- ✅ 11 HTTP 端点 × 6 服务覆盖(Agents × 8 / Tasks × 3 / Kernel × 2 / Handshake × 3 / Intent × 4 stub / Chat × 2)
- ✅ Bot 子包 3 platform(telegram / discord / webhook)+ `Bot` interface + `BotBuilder`
- ✅ ChatService 真实 2xx 示例(chatcmpl-787dcac6 / 11 tokens,引用 Stage 3.1 #4)
- ✅ 9 个 sentinel error + 9 个握手 sentinel + wau-edge 6 错误码完整列表
- ✅ 配置项 + 环境变量覆盖 + YAML 对照
- ✅ 类型定义完整(per types.go 1:1)
- ✅ Auth 完整示例 + JWT 7 字段说明

**WAU 业务代码改动 = 0**(纯文档,不改 .go)。
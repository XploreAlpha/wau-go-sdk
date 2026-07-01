# API 参考

> ⚠️ **本文档已迁移到 [../API.md](../API.md)(Stage 3.2 SDK doc 完整化,2026-07-01)**
>
> 旧版本(v0.6.0-preview.1 P1 阶段)仅保留作历史参考。
> **请使用最新版本** [../API.md](../API.md),包含:
> - 完整 11 端点 × 6 服务覆盖
> - ChatService(Stage 3.1 4 SDK 全 2xx 实测)
> - Bot 子包(telegram / discord / webhook per D13)
> - 9 sentinel error + 9 handshake sentinel + wau-edge 6 错误码
> - 配套 FAQ:[../FAQ.md](../FAQ.md)(10 通用 + 5 Go 特定)

---

# 历史版本(已迁移,仅作参考)

## Client

### `wau.New(baseURL, opts...)`

创建 SDK 客户端。

**参数**:
- `baseURL` (string): kernel HTTP 地址,例如 `http://localhost:18400`
- `opts` (...Option): 0 个或多个 `WithXxx()` 配置

**返回**:
- `*Client` (可并发安全)
- `error`: 配置错误(例如 `WithAuth` 缺 SharedSecret)

**示例**:
```go
c, err := wau.New("http://localhost:18400",
    wau.WithTimeout(10*time.Second),
    wau.WithAuth(wau.AuthConfig{...}),
)
```

### `(*Client).Close() error`

释放资源(M3.1 阶段 gRPC client 才需要)。当前是 no-op。

### `(*Client).BaseURL() string`

返回 base URL(debug / metrics 用)。

### `(*Client).CircuitState() string`

返回当前熔断状态:`"closed"` / `"open"` / `"half-open"`。

---

## KernelService

### `(*KernelService).Info(ctx) (*KernelInfo, error)`

`GET /kernel/info` — 返回 kernel 元信息。

### `(*KernelService).Health(ctx) (*HealthResponse, error)`

`GET /health` — 检查 kernel 健康(redis 连通性、版本、uptime)。

---

## AgentsService

### `(*AgentsService).Health(ctx) (*HealthResponse, error)`

`GET /health`(同 `KernelService.Health`,暴露在 Agents 上方便调用)。

### `(*AgentsService).List(ctx, opts) (*AgentListResponse, error)`

`GET /registry/agents?page=...&pageSize=...&skill=...&status=...&search=...`

`PageOptions` 字段:
- `Page` (int): 1-based 页码,默认 1
- `PageSize` (int): 默认 10
- `Skill` (string): 可选技能过滤
- `Status` (string): 可选状态过滤
- `Search` (string): 可选模糊匹配

### `(*AgentsService).Iter(ctx, opts) func(yield func(Agent, error) bool)`

Go 1.23+ 迭代器,遍历所有页。

```go
for a, err := range c.Agents().Iter(ctx, wau.PageOptions{Skill: "clinical"}) {
    if err != nil { ... }
    fmt.Println(a.Name)
}
```

### `(*AgentsService).Get(ctx, name) (*AgentStatus, error)`

`GET /registry/agents/{name}/status` — 综合状态(load + trust + circuit)。

### `(*AgentsService).Score(ctx, name) (*AgentScore, error)`

`GET /registry/agents/{name}/score` — 5 维评分(总分 + trust + skill + health + load)。

### `(*AgentsService).Register(ctx, req) error`

`POST /registry/agents/register` — 注册新 agent(RBAC: trusted_agent / kernel_core)。

```go
err := c.Agents().Register(ctx, wau.AgentRegisterRequest{
    Name:        "my-agent",
    URL:         "http://my-agent:18800",
    Description: "...",
    Skills:      []string{"clinical-decision-support"},
    Universes:   []string{"medical"},
})
```

### `(*AgentsService).Deregister(ctx, name) error`

`DELETE /registry/agents/{name}` — 注销 agent。

### `(*AgentsService).Heartbeat(ctx, agentID) error`

`POST /registry/agents/heartbeat` — agent 主动心跳上报(60s 一次)。

### `(*AgentsService).ReportLoad(ctx, agentID, load) error`

`POST /heartbeat/load` — 上报运行时负载(ActiveTasks / MaxCapacity / CPU / Memory)。

---

## TasksService

### `(*TasksService).Submit(ctx, req) (*SubmitResponse, error)`

`POST /registry/tasks/submit` — L4 真发 A2A。

```go
resp, err := c.Tasks().Submit(ctx, wau.SubmitRequest{
    Prompt:    "What is the capital of France?",
    TimeoutMs: 30000,
})
// resp.SelectedAgent, resp.Score, resp.Response, resp.A2ACallMs
```

### `(*TasksService).Simulate(ctx, req) (*DecisionInfo, error)`

`POST /registry/tasks/simulate` — L3 决策(不真发)。

### `(*TasksService).Get(ctx, taskID) (*Task, error)`

`GET /registry/tasks/{taskID}` — 查询任务详情。

---

## IntentService (M3.1 stub)

P2 阶段 stub,所有方法返 `wau.ErrNotImplemented`:
- `(*IntentService).Recommend(ctx, prompt, topK) (any, error)`
- `(*IntentService).ParseIntent(ctx, text) (any, error)`
- `(*IntentService).ListAgents(ctx, onlineOnly) (any, error)`
- `(*IntentService).HealthCheck(ctx) (any, error)`

---

## 类型 / DTO

所有 DTO 在 [`types.go`](../types.go)。字段以 **kernel 真相源**为准(参考 [ADR-0002](./adr/0002-sdk-stage-stratification.md))。

### SubmitRequest(关键修正)

```go
// 跟 wau-cli 旧 DTO 不同:SubmitRequest 只有 2 个字段
type SubmitRequest struct {
    Prompt    string `json:"prompt" binding:"required"`
    TimeoutMs int    `json:"timeout_ms,omitempty"`
}
```

wau-cli 旧 DTO(`{Message, SourcePeer, ...}`)已废弃,SDK 以 kernel 真相源为准。

### SubmitResponse

```go
type SubmitResponse struct {
    TaskID        string                  `json:"task_id"`
    Status        string                  `json:"status"`           // completed | failed | timeout
    SelectedAgent string                  `json:"selected_agent"`
    Score         float64                 `json:"score"`
    Decision      DecisionInfo            `json:"decision"`
    A2ACallMs     int                     `json:"a2a_call_ms"`
    Response      any                     `json:"response"`         // 中英兼容 string / object
    Error         string                  `json:"error,omitempty"`
}

type DecisionInfo struct {
    SelectedAgent  string      `json:"selected_agent"`
    Score          float64     `json:"score"`
    DecisionTimeMs int         `json:"decision_time_ms"`
    Candidates     []Candidate `json:"candidates,omitempty"`
}

type Candidate struct {
    Name   string  `json:"name"`
    Score  float64 `json:"score"`
    Reason string  `json:"reason"`
}
```

---

## 错误

所有错误实现 `error` 接口,可用 `errors.Is` 匹配 sentinel。

| Sentinel | 状态码 | 触发 |
|---|---|---|
| `wau.ErrNotFound` | 404 | 资源不存在 |
| `wau.ErrUnauthorized` | 401 | 鉴权失败 |
| `wau.ErrForbidden` | 403 | RBAC 不足 |
| `wau.ErrBadRequest` | 400 | 字段缺失 / 格式错 |
| `wau.ErrConflict` | 409 | 资源冲突 |
| `wau.ErrInternal` | 500 | kernel 端 panic |
| `wau.ErrCircuitOpen` | — | 熔断开 |
| `wau.ErrMaxRetries` | — | 重试耗尽(wraps last err) |
| `wau.ErrNotImplemented` | — | P2 stub |

`APIError` 含 `StatusCode` / `Code` / `Message` / `RequestID` / `Body`,用 `errors.As`:

```go
var apiErr *wau.APIError
if errors.As(err, &apiErr) {
    log.Printf("status=%d request_id=%s body=%s", apiErr.StatusCode, apiErr.RequestID, apiErr.Body)
}
```

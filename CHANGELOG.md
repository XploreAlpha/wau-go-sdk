# Changelog

## v0.6.0-preview.1 (2026-06-14)

> 🔶 Carnelian M3 W5 启动
> 抽取自 wau-cli/internal/client/(337 行,4 文件)

### 新增

- **HTTP API 11 端点 × 33 方法**(P1 阶段):
  - `KernelService`: Info + Health
  - `AgentsService`: Health + List + Iter + Get + Score + Register + Deregister + Heartbeat + ReportLoad
  - `TasksService`: Submit + Simulate + Get
  - `IntentService`: 4 个 gRPC stub(M3.1 实装,目前返 ErrNotImplemented)
- **typed errors**:`*APIError` + 8 个 sentinel(ErrNotFound / ErrUnauthorized / ErrCircuitOpen / ErrMaxRetries / ErrNotImplemented 等)
- **重试装饰器**:指数退避 + 抖动,默认 3 次 / 200ms-5s,只重试 5xx/429 + 网络错
- **熔断装饰器**:集成 wau-circuit 154 行(Go 版),per-Client 实例,5 failures / 30s recovery
- **HS256 鉴权**:JWT Bearer,5min exp,UUID v4 jti 防重放
- **SubmitRequest 字段修正**:以 kernel 真相源(`{Prompt, TimeoutMs}`)为准,**不是** wau-cli 旧 DTO(`{Message, SourcePeer, ...}`)
- **分页迭代器**:`Iter(ctx, opts) func(yield)` Go 1.23+ 泛型

### 测试

- **5 场景契约测试**:clinical / france / pain / sales / rare-disease(走 mock kernel + 黄金 JSON)
- **wau-circuit 9 个 table-driven 单测**:5 状态转移 + 变参 IsOpen + Reset + 并发安全 + String 格式化
- **retry / circuit / auth 单元测试**:30+ 个 test cases
- **集成测试**:TestClient_WithAuth_SetsBearerHeader + TestClient_NoAuth_NoAuthHeader

### 修复的真 bug

修复 wau-circuit 2 个真 bug(M3 W5.2 顺手):
- `NewBreaker(nil)` panic(无 nil check)
- `RecordFailure` 缺 `HalfOpen → Open` 转移(逻辑漏洞,HalfOpen 失败后 state 不动)

修复 wau-go-sdk 1 个架构 bug:
- 4 核心对象的所有方法**直接调** `c.tp.Get/Post`,**完全绕过** `Client.doWithRetry` 装饰器链(鉴权/熔断/重试全失效)。改为统一调 `c.doWithRetry(ctx, method, path, body, v)`

### 文档

- `README.md` — 项目状态 + 关联仓库
- `docs/quickstart.md` — 5 分钟接入
- `docs/api.md` — 完整 API 参考(11 端点 + DTO + 错误)
- `docs/auth.md` — HS256 鉴权指南
- `docs/retry_circuit.md` — 重试 + 熔断详解
- `docs/adr/0001-proto-sharing.md` — proto 共享策略(BSR + submodule 兜底)
- `docs/adr/0002-sdk-stage-stratification.md` — SDK P1/P2/P3 分层(M3 只做 P1)
- `docs/adr/0003-circuit-integration.md` — wau-circuit 集成(翻译到 3 SDK)
- `docs/adr/0004-contract-golden-location.md` — 5 场景契约归属(跟 Go SDK 仓共生)

### Examples

- `examples/list_agents/main.go` — 列出在线 agents
- `examples/submit_task/main.go` — 提交 L4 任务
- `examples/heartbeat_loop/main.go` — agent 端定时心跳(60s 间隔)
- `examples/five_scenarios/main.go` — 跑 5 场景契约

### 已知限制(P2/P3 推迟)

- ❌ gRPC client(P2):所有 `IntentService` 方法返 `ErrNotImplemented`
- ❌ A2A/AFP 协议层(P3):SDK 不暴露 Protocol interface
- ❌ 30 个 gRPC RPC(Scheduler / Scoring / Store / Circuit):P2 阶段做
- ❌ wau-cli 老 client 替换:等 M3 收尾(2026-06-22 ~ 07-05)

### 升级指引(从 wau-cli 老 client)

```diff
- import "wau-cli/internal/client"
+ import wau "github.com/wau/wau-go-sdk"

- client := client.NewClient(client.Options{...})
+ client, _ := wau.New("http://localhost:18400", wau.WithTimeout(...))

- resp, err := client.SubmitTask(ctx, &client.TaskSubmitRequest{
-     Message:    "...",
-     SourcePeer: "...",
- })
+ resp, err := client.Tasks().Submit(ctx, wau.SubmitRequest{
+     Prompt:    "...",
+     TimeoutMs: 30000,
+ })
```

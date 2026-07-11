## [Unreleased] — v1.3.0 "bot_uuid field add (W7.1, 2026-07-09)"

### Added

- `bot_uuid` (UUID v4, server-assigned) field added to `Account` + `RegisterBotRequest` structs in `bot/common/account.go`
- Per D78/D79/D80 decisions; D60 additive, 0 breaking change
- Cross-SDK JSON byte-equal alignment per D13
- 老 SDK v1.2.0 向后兼容(server 自动从 BotID slug 寻址并生成 bot_uuid)
- 0 unit tests added (W7.2 will add 15 mock e2e tests for 5 platforms × 3 cases)

### Compatibility

- 100% 保持向后兼容 — `bot_uuid` 字段是 `omitempty`,老 client 不传 = server 自动生成
- 老 9 字段(`account_id, tenant_id, bot_id, public_bot_id, owner_user_id, channel_type, channel_config_id, created_at, updated_at`) 0 改
- 老 `BotID` slug 语义不变(tenant-local, client-supplied)
- 跟 D66=B RBAC 兼容(`owner_user_id` 维持 string)

---

## [Unreleased] — v1.3.1 "L5 包管理器 client (W2.5, 2026-07-10, per D72/D73/D74)"

### Added

- `L5Service` + 5 client method(走 WAU-core-kernel /v1/l5/* HTTP API):
  - `Install(ctx, L5InstallRequest)`    装 agent(类比 apt install / npm install)
  - `Uninstall(ctx, L5UninstallRequest)` 卸 agent(purge=true 全删)
  - `Update(ctx, L5UpdateRequest)`       更新 agent(agent_name="" = 全更新)
  - `Search(ctx, L5SearchRequest)`       搜 wau-registry
  - `Login(ctx, L5LoginRequest)`         登入 WAU 账户
- 5 request/response struct + `L5SearchHit`
- `Client.L5()` getter(跟 `Client.Agents()` / `Client.Tasks()` / `Client.Kernel()` / `Client.Intent()` / `Client.Handshake()` / `Client.Chat()` / `Client.Skills()` 同模式)
- 9 unit tests(httptest + mock L5 server)覆盖 5 method 全 happy + 错误 path

### Compatibility (D60 additive)

- 0 改老 7 service(Agents/Tasks/Kernel/Intent/Handshake/Chat/Skills)
- 走 `c.doWithRetry`(自动 HS256 + 熔断 + 重试),跟其他 service 一致

### Reference

- D74 A 拍板(gRPC InstallAgent):[stage1/01-D66-D74-9-decisions-summary#九](https://github.com/wau-network/WAU-develop/blob/main/develop-log/kernel/v1.0.0/stage1/01-D66-D74-9-decisions-summary.md)
- 设计 doc:[stage1/04-wau-toolkit-v2.0-OS-level-design.md](https://github.com/wau-network/WAU-develop/blob/main/develop-log/kernel/v1.0.0/stage1/04-wau-toolkit-v2.0-OS-level-design.md)

---

## [Unreleased] — v1.3.2 "MCP client (W3, 2026-07-11, per D87)"

### Added

- 新 sub-package `mcpclient/`(独立 package,不走 `c.doWithRetry`):
  - `Client` + 7 sync tool wrapper(走 JSON-RPC 2.0 over HTTP,endpoint `POST {baseURL}/mcp`):
    - `HealthCheck(ctx, target)`                      → `map[string]any`{status,target}
    - `ParseAgentCard(ctx, raw)`                      → `*AgentCard`(raw 支持 string/[]byte/map)
    - `SendMessage(ctx, target, *Message)`            → `*Response`(Kind=message|task)
    - `GetTask(ctx, target, taskID)`                  → `*Task`
    - `ListTasks(ctx, target, *TaskFilter)`           → `*ListTasksResult`(tasks+total)
    - `CancelTask(ctx, target, taskID)`               → `*Task`(status.state=canceled)
    - `GetExtendedAgentCard(ctx, target)`             → `*ExtendedAgentCard`(含 capabilities)
  - 3 deferred placeholder(method 签名已就位,W4/W5 实装):
    - `CreateTaskPushNotificationConfig` (W4)
    - `StreamMessage` (W5, SSE)
    - `SubscribeToTask` (W5, SSE)
- 11 tool DTO:`AgentRef` / `Part` / `Message` / `Task` / `TaskStatus` / `Artifact` / `Response` / `AgentCard` / `ExtendedAgentCard` / `TaskFilter` / `ListTasksResult` / `PushNotificationConfig`
- `*RPCError` 类型(对齐 kernel `mcp.Error`):5 spec code + 3 MCP-specific code
- 4 functional option:`WithHTTPClient` / `WithBearerToken` / `WithUserAgent` / `WithEndpoint`
- 21 unit tests(httptest + mock MCP server)覆盖 7 tool 全 happy + 4 error path + 4 option + 3 lifecycle

### Compatibility (D60 additive)

- 0 改老 8 service(Agents/Tasks/Kernel/Intent/Handshake/Chat/Skills/L5)
- 0 改老根包导出 API
- 新 sub-package 独立 module path:`github.com/wau/wau-go-sdk/mcpclient`
- 走独立 `*http.Client`(caller 注入),不绑定 `Client.doWithRetry`(JSON-RPC error 走 200 + body envelope,跟 REST-shaped 错误不兼容)

### Reference

- D87 拍板(MCP server):[stage2/2026-07-10-D86-D87-D88-protocol-gateway-decision](https://github.com/wau-network/WAU-develop/blob/main/develop-log/kernel/v1.0.0/stage2/2026-07-10-D86-D87-D88-protocol-gateway-decision.md)
- 5 SDK MCP client 详设:[process/2026-07-10-W3-MCP-client-SDK-design](https://github.com/wau-network/WAU-develop/blob/main/develop-log/kernel/v1.0.0/process/2026-07-10-W3-MCP-client-SDK-design.md)
- MCP auth SDK design:[process/2026-07-11-W3-MCP-auth-SDK-design](https://github.com/wau-network/WAU-develop/blob/main/develop-log/kernel/v1.0.0/process/2026-07-11-W3-MCP-auth-SDK-design.md)

---

## [Unreleased] — v1.3.3 "UCP client (W3, 2026-07-11, per D88)"

### Added

- 新 sub-package `ucpclient/`(独立 package,跟 `mcpclient/` 平级,不走 `c.doWithRetry`):
  - `Client` + 11 commerce tool wrapper(走 JSON-RPC 2.0 over HTTP,endpoint `POST {baseURL}/ucp`):
    - `ListProducts(ctx, *ListProductsFilter) (*ListProductsResult, error)` — 列出商品(支持 category/price filter/pagination)
    - `GetProduct(ctx, productID) (*Product, error)` — 商品详情
    - `SearchProducts(ctx, query, limit) (*SearchProductsResult, error)` — 搜索商品
    - `AddToCart(ctx, productID, quantity) (*Cart, error)` — 加入购物车
    - `GetCart(ctx, cartID) (*Cart, error)` — 查看购物车
    - `RemoveFromCart(ctx, cartID, lineItemID) (*Cart, error)` — 移除商品
    - `CreateCheckoutSession(ctx, cartID) (*CheckoutSession, error)` — Stripe Checkout Session(W5+ Stripe)
    - `ConfirmPayment(ctx, checkoutSessionID) (*PaymentConfirmation, error)` — Stripe payment_intent 确认(W5+ Stripe)
    - `GetOrder(ctx, orderID) (*Order, error)` — 订单详情
    - `ListOrders(ctx, userID, *ListOrdersFilter) (*ListOrdersResult, error)` — 订单列表
    - `CancelOrder(ctx, orderID) (*CancelOrderResult, error)` — 取消订单(W5+ Stripe refund)
- 8 commerce DTO(Product / ListProductsFilter/Result / SearchProductsResult / CartLineItem / Cart / CheckoutSession / PaymentConfirmation / Order / ListOrdersFilter/Result / CancelOrderResult)— 含 `tenant_id` 字段(per D65 multi-tenant)
- 11 ToolXxx 常量(`ToolListProducts` … `ToolCancelOrder`)
- `*RPCError` 类型:5 spec code(JSON-RPC 2.0)+ 5 UCP-specific code(`-32101~ -32105` 跟 MCP `-32001~ -32003` 错开)
- `RPCError.As` / `IsNotFound(err)` / `IsStripeError(err)` helper(便于 caller 检查语义错误)
- `UcpAuth` + `SetBearerToken` + `SetTenantID`(per D78/D79/D80 bearer + D65 tenant 隔离)
- `IsStripePath(toolName)` Stripe 路径 helper(W5+ Stripe 集成期间 SDK 端不直接调 Stripe)
- 28 unit tests(httptest mock kernel + 11 tool round-trip + 错误 path + auth helper + stripe helper)100% PASS

### Compatibility (D60 additive)

- 0 改老 SDK(`mcpclient/` / `bot/` / `l5.go` / 其他老文件全部 0 改)
- 走独立 JSON-RPC 2.0 client(`callTool` 通用 dispatch),跟 `mcpclient.call` 类似但 endpoint 是 `/ucp`
- W3 stub:`CreateCheckoutSession` + `ConfirmPayment` 走 kernel `ErrNotImplemented`,handler 返 user-friendly 错误"W5 Stripe 集成中"
- Stripe SDK 0 直接依赖(等 kernel `ucp_stripe.go` 落地 W5+)

### Reference

- D88 拍板(UCP server):[stage2/2026-07-10-D86-D87-D88-protocol-gateway-decision](https://github.com/wau-network/WAU-develop/blob/main/develop-log/kernel/v1.0.0/stage2/2026-07-10-D86-D87-D88-protocol-gateway-decision.md)
- 5 SDK UCP client 详设:[process/2026-07-11-W3-UCP-client-SDK-design](https://github.com/wau-network/WAU-develop/blob/main/develop-log/kernel/v1.0.0/process/2026-07-11-W3-UCP-client-SDK-design.md)
- UCP Stripe design:[process/2026-07-11-W3-UCP-Stripe-Checkout-design](https://github.com/wau-network/WAU-develop/blob/main/develop-log/kernel/v1.0.0/process/2026-07-11-W3-UCP-Stripe-Checkout-design.md)
- benny 迁移澄清:kernel UCP 是通用 commerce 垂直协议层,benny 保持独立 demo plugin(2026-07-11 user 拍板)

---

## [Unreleased] — v1.0.0 "Phoenix" M11 W8 (2026-07-08)

### Added

#### M2 OAuth (ClientCredentials)
- `OAuthClient` + `TokenStore` + `RefreshHook`(sync + async)
- `transport_http.go` 新增 `authHeader()` getter
- `Client.SetOAuth()` additive(D60 老契约 0 改)
- 12 unit tests PASS

#### M11 P4 SkillsService (B 端 SDK)
- `SkillsService.List/Get/LoadForUser/ListForUser`
- `SkillsService.Publish(multipart upload, direct HTTP bypass retry)
- per `agentskills.io` D69=A manifest spec
- 5 unit tests PASS / D60 0 改老契约

### Compatibility

- API 100% 保留(Client / LLMDecision / SkillInfo / Transport / Auth 等老公开契约 0 改)
- 仅 additive 新增 OAuth + Skills 字段

---

## [v1.2.0] - 2026-07-02 (v0.9.0 GA)

### Highlights

- v1.2.0 (与 v0.9.0 "Acorn" 同步发版) + Stage 3.1 #10 Chat SSE streaming + 5 字段 100% 保留 + SDK doc 完整化
- 详见 GA 收口报告:~/WAU-develop/develop-log/kernel/v0.9.0/wrapup/2026-07-02-PROGRESS-v0.9.0-GA-CLOSURE.md

### Compatibility

- API 100% 保留
- LLMDecision 字段 100% 保留

# Changelog

## [v1.0.0] "Amber" - 2026-07-25 (W7.7 GA 文档校准)

> 🔷 **v0.7.0 "Amber" M3 W7.7 GA** — 文档校准为正式 v1.0.0 GA
> **状态**:✅ **v1.0.0 GA** — Public API stable
> **关联**:
> - [v0.7.0 milestone](/home/inamoto888/WAU-develop/develop-log/kernel/v0.7.0/milestone.md)
> - [W1-Day3 report](/home/inamoto888/WAU-develop/develop-log/kernel/v0.7.0/2026-06-15-W1-Day3.md)(Tracer 抽象)
> - [W7.7 acceptance](/home/inamoto888/WAU-develop/develop-log/kernel/v0.7.0/2026-07-25-W7-day7-5repos-finalize.md)

### Public API Stability

**The public API is stable since v1.0.0 (2026-07-25).**

Breaking changes follow SemVer 2.0.0:
- MAJOR for breaking, MINOR for feature, PATCH for fix

### Deprecation Policy

- Deprecated APIs supported for ≥ 1 minor version
- Compile-time warning with migration hint
- Listed in this CHANGELOG `### Deprecated` section

### Added(自 v0.6.0-preview.1)

- **OTel-compatible Tracer 抽象**(`Tracer` + `Span` interfaces + `noopTracer` 默认):
  - 用户实现 `wau.Tracer` 接口(adapter to OTel SDK)
  - `WithTracer(t Tracer)` option 注入
  - 不传 = `noopTracer{}` 默认(零依赖)
  - `doWithRetry` 自动包 `StartSpan` / `RecordError` / `SetAttribute` / `End`
  - 适配 OTel / OpenTracing / 自定义

### 测试

- `tracer_test.go` — 3 个单测(默认 noop / WithTracer 注入 / noop 零副作用)+ `stubTracer` 验证
- 跟既有 TestRetry / TestCircuit 测试不冲突
- `edgecase_test.go` — 5 edge case 单测(timeout / rate-limit / cancel / retry / 5xx)— W1 Day 6

### 战略意义

- ✅ **1.0.0 GA 验收** — 兼容 OTel 但不强制 import,符合 "OTel 是 optional 集成" 原则
- ✅ semver:**`Public API stable since v1.0.0 (2026-07-25)`**(W7.7 校准时正式声明)
- **跨 SDK 一致性**:wau-go-sdk / wau-python-sdk / wau-typescript-sdk / wau-rust-sdk 4/5 SDK 1.0 GA(KPI 9 v0.7.0)

### Manual Release 步骤(用户手动)

```bash
cd /home/inamoto888/project/wau-go-sdk
git status
git add -A
git commit -m "v0.7.0 W7.7: Go SDK v1.0.0 GA 文档校准 — Public API stable + deprecation policy"
git push origin main
git tag -a v1.0.0 -m "wau-go-sdk v1.0.0 — first stable release (Amber)"
git push origin v1.0.0
```

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

## [Unreleased] — v1.0.0 "Phoenix" M10 W8 (2026-07-08)

### Added

#### M10 N1 — Bot 注册 DTO + BotsService 公共接口

- `bot/common/account.go`(NEW,~102 行):
  - `Account` struct:AccountID / TenantID / BotID / PublicBotID / OwnerUserID / ChannelType / ChannelConfigID / CreatedAt / UpdatedAt / LastSeenAt
  - `NewAccount(tenantID, botID, ownerUserID, channelType, channelConfigID)` factory
  - `PublicBotIDOf(tenantID, botID)` — D82=A 派生 `bot:<tenant>:<botid>`
  - `RegisterBotRequest` / `UpdateBotRequest` / `ListBotsFilter` types
- `bot/common/bots_service.go`(NEW,~60 行):
  - `BotsService` interface:Register / Get / Update / List / Delete
  - 3 sentinel errors:ErrBotNotFound / ErrBotAlreadyExists / ErrInvalidArgument

#### Compatibility (D60)

- `bot.Bot` interface / `IncomingMessage` / `OutgoingMessage` / `BotBuilder` 全部 0 改
- 4 SDK 字段名 100% 一致(per D13)

#### M4 OAuth 增强 (2026-07-08)
- `RefreshableTokenStore.RefreshToken(ctx)` 公开方法(force=true 绕过双检,显式触发 refresh)
- `RefreshableTokenStore.CurrentPair() TokenPair` 返当前 token pair(明文,持久化用)
- `PKCEClient` + `PKCEConfig` + `GeneratePKCEChallenge()` Authorization Code + PKCE 公共 client 支持
- 0 改老 OAuthClient + 老 RefreshableTokenStore(D60 additive)
- 4 unit tests PASS(refreshToken + PKCE challenge + URL + exchange)

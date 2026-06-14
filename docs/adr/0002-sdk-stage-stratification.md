# ADR-0002: SDK 阶段分层(P1/P2/P3)

> **Status:** Accepted (2026-06-14)
> **Deciders:** Claude + youhaoxi
> **Context:** 3 SDK 暴露哪些 API 层给用户?一次性全做 vs 分阶段做?

## Context

WAU-core-kernel 当前暴露 3 层 API:

1. **HTTP REST**(端口 18400):11 个端点(`/health`, `/kernel/info`, `/registry/agents/*`, `/registry/tasks/*`, `/heartbeat/load`)
2. **gRPC**(端口 50051 + 50053):WauKernelService 16 RPC + IntentService 4 RPC + SchedulerService 7 RPC + ScoringService 3 RPC + StoreService 8 RPC + CircuitBreakerService 6 RPC = 44 RPC
3. **A2A/AFP 协议层**(v0.6.0 M1 完成,`internal/protocol/`):11 个 Protocol interface 方法(SendMessage / StreamMessage / GetTask / ListTasks / CancelTask / SubscribeToTask / CreateTaskPushNotificationConfig / GetExtendedAgentCard / HealthCheck / ParseAgentCard)

如果 3 SDK 一次性全部实现,工作量爆炸:
- HTTP 11 端点 × 3 SDK = 33 个方法,4.5 d/SDK
- gRPC 44 RPC × 3 SDK = 132 个 stub,1.5 d/SDK
- A2A/AFP 11 方法 × 3 SDK = 33 个,1 周/SDK(翻译 M1 protocol interface)

## Decision

**分 3 阶段:**

### P1(必做,M3 W5-W7 完成)
- HTTP API(11 端点)
- JSON DTO(手写,不依赖 proto)
- 鉴权(HS256 + Bearer)
- 重试(指数退避 + 抖动)
- 熔断(翻译 wau-circuit)
- **估时**:4.5 d/SDK × 3 = **13.5 d**
- **验收**:33 个方法全实现 + 5 场景契约测试 + wau-cli 替换

### P2(可选,M3.1 或 v0.7.0 完成)
- gRPC WauKernelService 16 RPC + IntentService 4 RPC(P2 最高优先级,其他 24 RPC 留 v0.7.0+)
- gRPC stub 用 buf 生成的 `.pb.go` / `.py` / `.ts`
- **估时**:1.5 d/SDK × 3 = **4.5 d**
- **验收**:20 个 RPC 全实现 + 契约测试

### P3(不进入 v0.6.0,推到 v0.7.0+)
- A2A/AFP 协议层(走 wau-a2a-gateway)
- WauKernelService 剩余 28 RPC(Scheduler/Scoring/Store/Circuit)
- Registry Peer/Card RPC
- 跨 universe 联邦调用
- **理由**:M1 protocol interface 刚落地,wau-a2a-gateway 还没稳定,不急

**M3 SDK 接口暴露:**
```go
// P1 — 必须
func (c *Client) Agents() *AgentsService
func (c *Client) Tasks() *TasksService
func (c *Client) Kernel() *KernelService

// P2 — stub 留口,返 ErrNotImplemented("P2 not in preview")
func (c *Client) Intent() *IntentService  // gRPC M3.1 实装
```

## Consequences

**正面:**
- M3 范围聚焦 P1,3 周可完成
- P2/P3 推后,等 wau-core-kernel gRPC 协议稳定后再做
- 投资人不失望(P1 已能让 Python/TS 用户接入)
- 接口分层清晰,preview 阶段不承诺 P3

**负面:**
- Python/TS 用户在 P1 阶段只能用 HTTP 调 SDK,不能调 gRPC(IntentService 走 gRPC 50053)
  - **缓解**:Kernel 内 P1 阶段 HTTP /registry/tasks/submit 已能调 L4(IntentService 通过 kernel 中转,P1 阶段不直连)
- P2 gRPC stub 留口,API 易变(M3.1 阶段可能改签名)
  - **缓解**:preview.1 → preview.2 间隔 ≤ 1 月,改签名可控

**Risk:**
- P3 推到 v0.7.0,可能 user 抱怨"SDK 不完整"
  - **缓解**:README + CHANGELOG 明确写"v0.6.0-preview 仅 P1,P2 在 M3.1,P3 在 v0.7.0+"

## Alternatives Reconsidered

- 一次性 P1+P2+P3 全部做:不可行,3 周干不完
- 跳过 P1 直接 P2:不推荐,99% 业务用 HTTP,gRPC 走 P2 是性能优化
- P1+P2 一起做(P3 推后):4-5 周工作量,3 周干不完,只能 M3.1 收尾

## References

- plan: [`lexical-orbiting-nova.md` §9 决策 2](../../../../../../.claude/plans/lexical-orbiting-nova.md)
- 协议层: [`WAU-core-kernel/internal/protocol/protocol.go`](../../../../../WAU-core-kernel/internal/protocol/protocol.go)
- HTTP 端点: [`WAU-core-kernel/cmd/wau-core/main.go:283-315`](../../../../../WAU-core-kernel/cmd/wau-core/main.go#L283-L315)

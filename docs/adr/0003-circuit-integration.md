# ADR-0003: wau-circuit 集成到 3 SDK(翻译 vs RPC 转发)

> **Status:** Accepted (2026-06-14)
> **Deciders:** Claude + youhaoxi
> **Context:** wau-circuit 是 Go 写的熔断器,3 SDK 怎么用?

## Context

[wau-circuit/breaker.go](../../../../../wau-circuit/breaker.go) 是 WAU 自家 Go 写的熔断器,154 行,纯 stdlib,无外部依赖,3 状态(Closed/Open/HalfOpen)状态机。

3 SDK 需要熔断功能,**核心接口**:
```go
type CircuitRecorder interface {
    RecordSuccess(agentID string)
    RecordFailure(agentID string)
    IsOpen(agentID ...string) bool
}
```

(在 [handle_submit_l4.go:68-77](../../../../../WAU-core-kernel/cmd/wau-core/handle_submit_l4.go#L68-L77) 定义)

**3 个候选方案:**

| 方案 | 优点 | 缺点 |
|---|---|---|
| A. 直接翻译到各 SDK | 离线可用;SDK 自包含;无跨进程;语言原生 | 三处维护(Go/Python/TS) |
| B. 走 gRPC RPC 转发 | 单一真相源;改一处全 SDK 同步 | 强依赖 wau-core-kernel 进程;SDK 离线用不了;延迟高 |
| C. 独立 sidecar 进程 | 单一真相源;可独立扩展 | 部署复杂;SDK 启动要多一个进程 |

## Decision

**选 A(直接翻译)**。

具体:
1. **Go SDK** (`wau-go-sdk/circuit.go`):直接 `import "github.com/wau/circuit"`,无翻译成本
2. **Python SDK** (`wau-python-sdk/src/wau_sdk/_circuit.py`):逐行翻译 wau-circuit Go 代码到 Python,~150 行
3. **TypeScript SDK** (`wau-typescript-sdk/src/circuit.ts`):逐行翻译到 TypeScript,~150 行
4. **行为对齐测试**:`tests/test_circuit_parity.go`(3 SDK 同一组故障注入用例,断路时序必须 1:1)
5. **v0.6.0 之前额外工作**(M3 W5.2 已做):补 wau-circuit 9 个 table-driven 单测,SDK 翻译时对齐行为有依据
6. **已修的 2 个 wau-circuit bug**(M3 W5.2 顺手):
   - `NewBreaker(nil)` nil logger panic
   - `RecordFailure` 缺 `HalfOpen → Open` 转移(逻辑漏洞)

## Consequences

**正面:**
- SDK 离线可用,自包含,部署简单
- 用户调 SDK 不需先启动 wau-core-kernel
- 三语言熔断器独立,无网络依赖
- 行为对齐后,语义一致(契约测试保证)

**负面:**
- 3 处维护成本(wau-circuit 改时,3 SDK 都要跟)
  - **缓解**:行为对齐测试 1:1 强制;weekly doc review
- 状态不跨进程共享(每个 SDK 实例自己记状态)
  - **缓解**:SDK 实例粒度足够细(1 应用 1 SDK 实例),不需跨进程

**Risk:**
- 行为漂移:3 SDK 翻译后行为不一致
  - **缓解**:plan §10.5 "重试 + 熔断行为黄金测试" 兜底(注入同一组故障,断路时序必须 1:1)

## Alternatives Reconsidered

- 选 B(gRPC RPC)被排除:SDK 离线用不了,延迟高(每请求多一跳)
- 选 C(sidecar)被排除:部署复杂(3 SDK 用户多 1 进程),wau-circuit 154 行不值得

## References

- plan: [`lexical-orbiting-nova.md` §9 决策 3](../../../../../../.claude/plans/lexical-orbiting-nova.md)
- wau-circuit: [`/home/inamoto888/project/wau-circuit/breaker.go`](../../../../../wau-circuit/breaker.go)
- wau-circuit 单测(9 个,含 2 个 bug 修复): [`/home/inamoto888/project/wau-circuit/breaker_test.go`](../../../../../wau-circuit/breaker_test.go)
- 接口定义: [`handle_submit_l4.go:68-77`](../../../../../WAU-core-kernel/cmd/wau-core/handle_submit_l4.go#L68-L77)

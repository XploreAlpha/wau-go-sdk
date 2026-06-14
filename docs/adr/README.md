# v0.6.0 M3 架构决策记录(ADR)

> 本目录记录 v0.6.0 M3 (3 SDK 启动) 期间的关键架构决策。
> 每份 ADR 用 MADR 风格:Context / Decision / Consequences / Alternatives / References。

## 决策清单

| # | 标题 | 状态 | 日期 |
|---|------|------|------|
| [0001](./0001-proto-sharing.md) | proto 共享策略(选 A: BSR + C 兜底) | ✅ Accepted | 2026-06-14 |
| [0002](./0002-sdk-stage-stratification.md) | SDK 阶段分层(P1/P2/P3,M3 只做 P1) | ✅ Accepted | 2026-06-14 |
| [0003](./0003-circuit-integration.md) | wau-circuit 集成(选 A: 翻译到 3 SDK) | ✅ Accepted | 2026-06-14 |
| [0004](./0004-contract-golden-location.md) | 5 场景契约测试归属(选 B: 跟 Go SDK 共生) | ✅ Accepted | 2026-06-14 |

## 决策摘要

**proto 共享**: 本地开发走 `buf.gen.yaml` + `inputs.directory`,CI 走 BSR (`buf.build/wau/wau-apis`),兜底 git submodule。P1 阶段不依赖 proto。

**SDK 分层**: M3 只做 P1 (HTTP 11 端点 + JSON DTO),P2 (gRPC 20 RPC) 推 M3.1,P3 (A2A/AFP 协议层) 推 v0.7.0+。

**熔断集成**: 直接翻译 wau-circuit 154 行 Go 代码到 Python/TS(各 ~150 行),Go SDK 直接 import。3 SDK 行为对齐由"故障注入黄金测试"兜底。

**契约测试归属**: 5 场景黄金 JSON 放 `wau-go-sdk/tests/contract-golden/`,Python/TS SDK 通过 git submodule 引用。

## 关联

- M3 计划: [`/home/inamoto888/.claude/plans/lexical-orbiting-nova.md`](file:///home/inamoto888/.claude/plans/lexical-orbiting-nova.md)
- M2 验收报告: [`/home/inamoto888/WAU-develop/develop-log/kernel/v0.6.0/2026-06-13-M2-full-acceptance.md`](file:///home/inamoto888/WAU-develop/develop-log/kernel/v0.6.0/2026-06-13-M2-full-acceptance.md)

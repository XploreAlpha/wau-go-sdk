# ADR-0001: proto 共享策略(3 SDK 跨仓)

> **Status:** Accepted (2026-06-14)
> **Deciders:** Claude + youhaoxi
> **Context:** v0.6.0 M3 启动,3 SDK(Go/Python/TypeScript)需要共享 WAU-core-kernel 的 8 个 .proto

## Context

WAU-core-kernel 当前有 8 个 .proto(已在 `WAU-core-kernel/proto/`,buf v2 已配):

| proto 包 | 用途 | 备注 |
|---|---|---|
| `wau.kernel.v1` | WauKernelService 16 RPC | HTTP/gRPC 入口 |
| `wau.registry.v1` | registry 6 Peer + 4 Card RPC | P2 阶段 |
| `wau.scheduler.v1` | 任务调度 RPC | P2 阶段 |
| `wau.scoring.v1` | 15 维评分 | P2 阶段 |
| `wau.store.v1` | Trust + Task 持久化 | P2 阶段 |
| `wau.circuit.v1` | 熔断器状态 | P2 阶段 |
| `wau.intent.v1` | wau-intent-service 4 RPC | **M2 W4.1-4.3 改过** |
| `wau.discovery.v1` | 过渡期 discovery | ⚠️ 即将废弃 |

3 个 SDK 仓(`wau-go-sdk` / `wau-python-sdk` / `wau-typescript-sdk`)需要从这些 .proto 生成对应语言的类型/stub。问题:怎么共享?

**4 个候选方案:**

| 方案 | 优点 | 缺点 |
|---|---|---|
| A. buf remote (BSR) | 单一真相源,proto 改一处全 SDK 同步;`buf breaking` 自动化 | CI 起步慢 30s;需 BSR 账号 |
| B. 复制 .pb.go 到各 SDK | CI 快;零外部依赖 | 4 份重复代码,proto 改要手动同步 |
| C. git submodule | 简单;Git 原生 | submodule 维护体验差,新人 onboard 难 |
| D. Go module 依赖 wau-core-kernel | 类型强一致 | 用户被强迫拉整个 kernel 依赖图(几 MB) |

## Decision

**选 A(buf remote / BSR)作为主路径,选 C(git submodule)作为兜底**。

具体:
1. **本地开发**(`wau-go-sdk/buf.gen.yaml` 已配):用 `inputs.directory: ../WAU-core-kernel/proto`,buf 1.70.0 已验证可行(2026-06-14 试过,生成 16 个 .pb.go 共 524K,`.gitignore` 生效)
2. **CI / 发版**:`buf generate buf.build/wau/wau-apis` — 需要在 BSR 上有 `wau-apis` 镜像(待 youhaoxi 配置 BSR 账号)
3. **兜底**:BSR 不可用时,改用 `git submodule add https://github.com/wau/wau-core-kernel.git vendor/wau-core-kernel`,`buf.gen.yaml` 改用 `inputs.directory: vendor/wau-core-kernel/proto`
4. **P1 阶段不依赖 proto**:M3 范围 11 HTTP 端点手写 DTO 即可,proto 仅给 P2 阶段 gRPC stub 用
5. **`.gitignore` 必须含 `/gen/`** — 524K 生成物不入库

## Consequences

**正面:**
- proto 改一处全 SDK 同步,无人工同步成本
- BSR 镜像有版本历史,可回滚
- P1 阶段不阻塞,先发版再说

**负面:**
- BSR 配置需要 youhaoxi 1 次性配置账号(估 30 min)
- 兜底 git submodule 需 CLI 用户熟悉 submodule
- 本地开发路径 `../WAU-core-kernel/proto` 在 CI 走不通,必须切 BSR 或 submodule

**Risk:**
- BSR 不可用 → 立即切 submodule
- `buf breaking` 报错阻 SDK 升级 → 协调 wau-core-kernel 改 proto 兼容性

## Alternatives Reconsidered

- 选 D(Go module 依赖)被排除:用户用 SDK 时被迫拉整个 kernel 依赖图,体积爆炸。
- 选 B(复制)被排除:3 份 .pb.go 漂移风险高,后续维护成本爆炸。

## References

- plan: [`/home/inamoto888/.claude/plans/lexical-orbiting-nova.md` §9 决策 1](../../../../../../.claude/plans/lexical-orbiting-nova.md)
- buf v2 文档: https://buf.build/docs/configuration/v2/buf-gen-yaml
- 验证记录: 2026-06-14,本地 `buf generate --template buf.gen.yaml` 跑通

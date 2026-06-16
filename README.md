# wau-go-sdk

> **WAU Go SDK v1.0.0 GA** — 官方 Go 客户端,WAU-core-kernel 智能调度内核接入入口
> v0.7.0 "Amber" 🔷 — **v1.0.0 = 2026-07-25 GA**(W7.7 完成)

[![Go Reference](https://pkg.go.dev/badge/github.com/wau/wau-go-sdk.svg)](https://pkg.go.dev/github.com/wau/wau-go-sdk)
[![Version](https://img.shields.io/badge/version-v1.0.0-blue?style=flat-square)](https://github.com/wau/wau-go-sdk/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

## 状态

✅ **v1.0.0 GA** (2026-07-25) — **Public API stable**

| Phase | 状态 |
|-------|------|
| W5.1 go.mod + CI + 基础脚手架 | ✅ |
| W5.2 wau-circuit 补单测(地基) | ✅ |
| W5.3 Client + Agents + Tasks + Kernel 4 核心对象 | ✅ |
| W5.4 熔断 + 重试装饰器 | ✅ |
| W5.5 HS256 鉴权 | ✅ |
| W5.6 单测 + 5 场景契约 + 真 kernel 联调 | ✅ |
| W5.7 README + docs + examples | ✅ |
| W5.8 OTel Tracer 抽象(W1 M3 Day 3) | ✅ |
| W1 Day 6 5 edge case 测试(timeout / rate-limit / cancel / retry / 5xx) | ✅ |
| W7.7 Public API stable + deprecation policy | ✅ |
| tag v1.0.0 + pkg.go.dev | ⏳ 用户手动 |

## 安装

```bash
go get github.com/wau/wau-go-sdk@v1.0.0
```

## Public API 速览

```go
client, _ := wau.New("http://localhost:18400",
    wau.WithTimeout(30*time.Second),
    wau.WithAuth("your-token"),
    wau.WithTracer(myTracer),  // 可选 OTel adapter
)

// 4 核心服务
client.Kernel().Info(ctx)              // GET /kernel/info
client.Agents().List(ctx)              // GET /registry/agents
client.Agents().Get(ctx, "Whis")       // GET /registry/agents/{name}
client.Tasks().Submit(ctx, req)        // POST /registry/tasks/submit
client.Intent().Parse(ctx, prompt)     // gRPC stub(M3.1 实装)
```

## 关联仓库

- 上游: [wau-core-kernel](https://github.com/wau/wau-core-kernel) (HTTP :18400, gRPC :50051)
- 兄弟: [wau-python-sdk](https://github.com/wau/wau-python-sdk) | [wau-typescript-sdk](https://github.com/wau/wau-typescript-sdk) | [wau-rust-sdk](https://github.com/wau/wau-rust-sdk) — 4/5 SDK 1.0 GA
- 抽取源: [wau-cli/internal/client/](https://github.com/wau/wau-cli/tree/main/internal/client) (337 行)
- 复用: [wau-circuit](https://github.com/wau/wau-circuit) (熔断器)

## 计划文档

v0.7.0 完整计划:[`/home/inamoto888/WAU-develop/develop-log/kernel/v0.7.0/milestone.md`](file:///home/inamoto888/WAU-develop/develop-log/kernel/v0.7.0/milestone.md)

## 协议

MIT © 2026 youhaoxi

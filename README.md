# wau-go-sdk

> WAU Go SDK v0.6.0-preview.1 — 官方 Go 客户端,WAU-core-kernel 智能调度内核接入入口

[![Go Reference](https://pkg.go.dev/badge/github.com/wau/wau-go-sdk.svg)](https://pkg.go.dev/github.com/wau/wau-go-sdk)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

## 状态

🚧 **v0.6.0-preview.1** (W5 实施中,2026-06-13 启动,预计 06-19 完)

| Phase | 状态 |
|-------|------|
| 启动前 4 件事(建仓/buf remote/mock/ADR)| 🔵 进行中 |
| W5.1 go.mod + CI + 基础脚手架 | 🔵 进行中 |
| W5.2 wau-circuit 补单测(地基) | 📋 待启动 |
| W5.3 Client + Agents + Tasks + Kernel 4 核心对象 | 📋 待启动 |
| W5.4 熔断 + 重试装饰器 | 📋 待启动 |
| W5.5 HS256 鉴权 | 📋 待启动 |
| W5.6 单测 + 5 场景契约 + 真 kernel 联调 | 📋 待启动 |
| W5.7 README + docs + examples | 📋 待启动 |
| W5.8 tag v0.6.0-preview.1 + pkg.go.dev | 📋 待启动 |

## 关联仓库

- 上游: [wau-core-kernel](https://github.com/wau/wau-core-kernel) (HTTP :18400, gRPC :50051)
- 兄弟: [wau-python-sdk](https://github.com/wau/wau-python-sdk) | [wau-typescript-sdk](https://github.com/wau/wau-typescript-sdk)
- 抽取源: [wau-cli/internal/client/](https://github.com/wau/wau-cli/tree/main/internal/client) (337 行)
- 复用: [wau-circuit](https://github.com/wau/wau-circuit) (熔断器)

## 计划文档

v0.6.0 M3 完整计划:[`/home/inamoto888/.claude/plans/lexical-orbiting-nova.md`](file:///home/inamoto888/.claude/plans/lexical-orbiting-nova.md)

## 协议

MIT © 2026 youhaoxi

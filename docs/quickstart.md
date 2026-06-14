# Quickstart

5 分钟接入 WAU 智能调度内核。

## 安装

```bash
go get github.com/wau/wau-go-sdk@v0.6.0-preview.1
```

## Hello World

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    wau "github.com/wau/wau-go-sdk"
)

func main() {
    c, err := wau.New("http://localhost:18400",
        wau.WithTimeout(10*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer c.Close()

    resp, err := c.Tasks().Submit(context.Background(), wau.SubmitRequest{
        Prompt:    "What is the capital of France?",
        TimeoutMs: 30000,
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("✅ 选中 %s: %v\n", resp.SelectedAgent, resp.Response)
}
```

## 核心 4 服务

```go
c.Kernel()   // /kernel/info + /health
c.Agents()   // /registry/agents/* (CRUD + 状态 + 评分 + 心跳)
c.Tasks()    // /registry/tasks/* (submit + simulate + get)
c.Intent()   // gRPC (M3.1 实装,目前返 ErrNotImplemented)
```

## 常用配置

```go
c, _ := wau.New("http://localhost:18400",
    wau.WithTimeout(30*time.Second),     // 默认 30s
    wau.WithRetryNo(),                   // 关闭重试(默认 3 次指数退避)
    wau.WithCircuitDisabled(),           // 关闭熔断(默认 5 failures 触发)
    wau.WithAuth(wau.AuthConfig{         // 启用 HS256
        AgentName:    "my-agent",
        SharedSecret: []byte(os.Getenv("WAU_JWT_SECRET")),
        Role:         wau.RoleTrustedAgent,
    }),
    wau.WithLogger(slog.Default()),      // 自定义 logger
)
```

## 错误处理

```go
resp, err := c.Tasks().Submit(ctx, req)
if err != nil {
    var apiErr *wau.APIError
    if errors.As(err, &apiErr) {
        switch {
        case errors.Is(err, wau.ErrNotFound):
            // 404 业务处理
        case errors.Is(err, wau.ErrUnauthorized):
            // 401 重启鉴权
        case errors.Is(err, wau.ErrCircuitOpen):
            // 熔断开,等 30s 再试
        case errors.Is(err, wau.ErrMaxRetries):
            // 重试耗尽,上报监控
        }
    }
}
```

## 下一步

- [API 参考](./api.md) — 全部 11 端点 + DTO
- [鉴权 (HS256 + JWT)](./auth.md) — Bearer token 注入
- [重试 + 熔断](./retry_circuit.md) — 指数退避 + wau-circuit 集成
- [examples/](../examples/) — 4 个可运行示例
- [ADR-0001 ~ 0004](./adr/) — 架构决策记录

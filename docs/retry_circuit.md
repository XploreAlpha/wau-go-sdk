# 重试 + 熔断

wau-go-sdk 默认启用两个保护性装饰器,自动应用到所有 4 核心对象的 HTTP 调用。

## 重试(指数退避 + 抖动)

**默认配置**(可在 `WithRetry()` 覆盖):

| 字段 | 默认值 | 说明 |
|---|---|---|
| `MaxRetries` | 3 | 总共调 4 次(1 + 3 重试) |
| `InitialBackoff` | 200ms | 第一次退避 |
| `MaxBackoff` | 5s | 退避上限 |
| `Jitter` | 0.2 | ±20% 随机抖动(防雪崩) |
| `RetryOn` | `[500, 502, 503, 504, 429]` | 触发重试的状态码 |

**重试规则**:
- 5xx + 429 → 重试
- 4xx(非 429)→ **不**重试(业务错,服务端没责任)
- 网络错 / 超时 → 重试
- `context.Canceled` / `DeadlineExceeded` → 立即停止,不重试

**总耗时估算**:3 次重试,backoff 200ms + 400ms + 800ms = **~1.4s**(无 jitter)

**关闭重试**:
```go
c, _ := wau.New("http://localhost:18400", wau.WithRetryNo())
```

**自定义**:
```go
c, _ := wau.New("http://localhost:18400", wau.WithRetry(wau.RetryConfig{
    MaxRetries:     5,
    InitialBackoff: 100 * time.Millisecond,
    MaxBackoff:     10 * time.Second,
    Jitter:         0.3,
    RetryOn:        []int{502, 503, 504},
}))
```

## 熔断(集成 wau-circuit)

**默认配置**(`WithCircuit()` 可覆盖):

| 字段 | 默认值 | 说明 |
|---|---|---|
| `Enabled` | true | 总开关 |
| `FailureThreshold` | 5 | 5 次失败后开熔断 |
| `OpenTimeout` | 30s | 熔断开 30s 后转 HalfOpen |

**熔断状态机**(参考 [wau-circuit](https://github.com/wau/wau-circuit)):

```
Closed ──(5 failures)──> Open
   ^                        │
   │                        │ 30s 超时
   │                        ▼
   └─(1 success)─── HalfOpen
                       │
                       │ 1 failure
                       ▼
                     Open
```

**短路行为**:
- Open 状态下,所有 HTTP 请求立即返 `wau.ErrCircuitOpen`(不调底层 transport)
- 节省 kernel 端的无效请求

**记录规则**(`isCircuitFailure`):
- 5xx → 计入失败
- 4xx → **不**计入(业务错,服务可用)
- 网络错 / 超时 → 计入

**查询当前状态**(debug / metrics):
```go
state := c.CircuitState() // "closed" | "open" | "half-open"
```

**关闭熔断**(测试 / 调试):
```go
c, _ := wau.New("http://localhost:18400", wau.WithCircuitDisabled())
```

## 装饰器链调用顺序

```
Caller → Client.doWithRetry → Retrier.Do → Circuit.Guard → Transport.do → HTTP
              ↑                    ↑              ↑              ↑
            重试              指数退避       熔断短路       Bearer JWT
```

所有 4 核心对象(`Agents` / `Tasks` / `Kernel`)都自动走这条链。

## 集成测试示例

```go
// 重试触发场景:kernel 第一次返 503,第二次返 200
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if atomic.AddInt32(&calls, 1) == 1 {
        w.WriteHeader(503) // 第 1 次失败
        return
    }
    w.WriteHeader(200)
    w.Write([]byte(`{"status":"ok","version":"v0.6.0","uptime":1.0}`))
}))
defer srv.Close()

c, _ := wau.New(srv.URL, wau.WithRetry(wau.RetryConfig{
    MaxRetries:     3,
    InitialBackoff: 10 * time.Millisecond,
    MaxBackoff:     100 * time.Millisecond,
    Jitter:         0,
    RetryOn:        []int{503},
}))

h, _ := c.Kernel().Health(ctx)
// calls 应 = 2(1 失败 + 1 成功)
```

## 行为对齐

wau-go-sdk 的熔断行为跟 [wau-circuit](https://github.com/wau/wau-circuit) **字节级一致**。Python/TS SDK 的熔断器翻译 wau-circuit 154 行 Go 代码,行为对齐由 5 场景契约测试 + 故障注入黄金测试兜底(详见 [ADR-0003](./adr/0003-circuit-integration.md))。

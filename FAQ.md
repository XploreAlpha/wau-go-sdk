# wau-go-sdk 故障排查(FAQ)

> **版本**:v1.1.0(v0.9.0 "Acorn" Stage 3.2,2026-07-01)
> **范围**:10 通用问题 + 5 Go 语言特定问题 = 15 Q&A
> **配套**:`docs/retry_circuit.md`(重试熔断详解)+ `docs/auth.md`(鉴权详解)

---

## 通用问题(10 Q,跨 4 SDK 适用)

### Q1: 401 Unauthorized / invalid tenant

**症状**:
```
wau: unauthorized (status 401, code=unauthorized, request_id=...): {"error":"tenant_id missing"}
```

**原因**:
- `Signer` 没签 `tenant_id` claim(per Stage 3.1 #1 修复前的旧 bug)
- `wau-edge` JWT secret 跟 SDK `SharedSecret` 不一致
- `AuthConfig.TenantID` 为空字符串

**修复**:
```go
// 1. 必填 TenantID(空字符串 → newSigner 返错)
wau.WithAuth(wau.AuthConfig{
    Role:         wau.RoleExternalAgent,
    AgentName:    "my-agent",
    TenantID:     "tenant-A", // ← 必填,per Stage 3.1 #1
    SharedSecret: []byte(os.Getenv("WAU_EDGE_JWT_SECRET")),
})

// 2. JWT secret 一致(server 端 wau-edge/internal/auth/jwt.go 校验)
//    wau-edge 默认空 secret = 严格 reject(per #1+#2 修复)
//    必须 env WAU_EDGE_JWT_SECRET=xxx 启动 wau-edge

// 3. 验证
curl -H "Authorization: Bearer $TOKEN" http://localhost:18402/v1/chat/completions
# 期望:200
```

**进度报告**:[[2026-07-01-PROGRESS-M5-#1+2-blocker-fix]]

---

### Q2: connection refused :18402 / :18400 / :18404

**症状**:
```
wau: http do: dial tcp 127.0.0.1:18402: connect: connection refused
```

**原因**:wau-core-kernel(:18400)/ wau-edge(:18402)/ wau-llm-router(:18404)未启。

**修复**:
```bash
# 走 §3.8 onelab 脚本(4 步基线)
bash /home/inamoto888/WAU-develop/develop-log/kernel/v0.9.0/v0.9.0-onelab-deploy.sh up

# 或单独启
cd /home/inamoto888/project/wau-edge
go run ./cmd/wau-edge -config configs/edge.yaml

# 验证端口活
curl 127.0.0.1:18402/health
grpcurl 127.0.0.1:18404 list
ss -tlnp | grep -E ":1840[0-4]"
```

**端口速查**:

| 服务 | HTTP | gRPC | 备注 |
|---|---|---|---|
| wau-core-kernel | :18400 | :18401 | SDK baseURL 默认 |
| wau-edge | :18402 | :18403 | Chat completions 用 |
| wau-llm-router | :18403 | :18404 | HTTP/gRPC 不同协议层不冲突 |

---

### Q3: context deadline exceeded / timeout

**症状**:
```
wau: context deadline exceeded
```

**原因**:`WithTimeout(d)` 配置的 d < kernel 处理时间。

**修复**:
```go
// 1. 调高 timeout(默认 30s)
c, _ := wau.New("http://localhost:18400",
    wau.WithTimeout(60*time.Second),
)

// 2. 单次请求传 ctx(覆盖全局 timeout)
ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
defer cancel()
resp, err := c.Tasks().Submit(ctx, req)

// 3. 长 task 用 SubmitRequest.TimeoutMs
resp, _ := c.Tasks().Submit(ctx, wau.SubmitRequest{
    Prompt:    "long task",
    TimeoutMs: 300000, // 5 分钟
})
```

---

### Q4: Bot.Start() 卡住 / 不响应

**症状**:`bot.Start(ctx)` 返回 nil 但 Telegram/Discord 不响应消息。

**原因**:
- Bot token 没设 / 不对
- 网络不通(国内访问 Telegram API 需代理)
- `OnMessage` 没注册 handler

**修复**:
```bash
# 1. 检查 token
echo $TELEGRAM_BOT_TOKEN | head -c 10 # 应该以数字开头(格式:123456:ABC-DEF...)

# 2. 检查网络
curl https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/getMe
# 国内:挂代理 export https_proxy=http://127.0.0.1:7890

# 3. SDK 端确认 OnMessage 注册
bot.OnMessage(func(in botcommon.IncomingMessage) botcommon.OutgoingMessage {
    return botcommon.OutgoingMessage{Text: "echo: " + in.Text}
})
```

---

### Q5: 重试耗尽 / 熔断开

**症状**:
```
wau: max retries exceeded
wau: circuit breaker is open
```

**原因**:上游 5xx / 网络抖动超过阈值。

**修复**:
```go
// 1. 调高 retry 阈值(默认 3 次)
c, _ := wau.New("http://localhost:18400",
    wau.WithRetry(wau.RetryConfig{
        MaxRetries:     5,
        InitialBackoff: 500 * time.Millisecond,
        MaxBackoff:     10 * time.Second,
        RetryOn:        []int{500, 502, 503, 504, 429},
    }),
)

// 2. 调高熔断阈值(默认 5 失败)
c, _ = wau.New("http://localhost:18400",
    wau.WithCircuit(wau.CircuitConfig{
        FailureThreshold: 10,
        OpenTimeout:      60 * time.Second,
    }),
)

// 3. 临时禁用(测试用)
c, _ = wau.New("http://localhost:18400",
    wau.WithRetryNo(),
    wau.WithCircuitDisabled(),
)

// 4. 检查当前熔断状态
fmt.Println(c.CircuitState()) // "closed" / "open" / "half-open"
```

**详细**:[docs/retry_circuit.md](./docs/retry_circuit.md)

---

### Q6: chat completions 返回 404 model_not_found

**症状**:
```
wau: model not found (status 404, code=MODEL_NOT_FOUND): ...
```

**原因**:`Model` 字段不在 wau-llm-router universe 配置里。

**修复**:
```go
// Stage 1 MockModels 唯一接受 model 名(per §3.7 实测)
resp, _ := c.Chat().Completions(ctx, wau.ChatCompletionRequest{
    Model:    "wau-default", // ← Stage 1 唯一接受
    Messages: []wau.ChatMessage{{Role: "user", Content: "hi"}},
})

// Stage 2 后真模型(gpt-4o / claude-haiku 等)需 wau-llm-router 配 universe
// per wau-llm-router/configs/router.yaml
```

---

### Q7: Thompson Update 失败 / reward out of range

**症状**(v1.0.0 才会触发,本期不适用):
```
wau: thompson: reward out of range [0,1]
```

**原因**:reward 超 [0,1] 范围。

**修复**:
```go
// reward ∈ [0, 1](v1.0.0 实装后)
update := wau.UpdateInput{
    Model:  "gpt-4o-mini",
    Reward: 0.85, // 必须在 [0, 1]
}
```

---

### Q8: SDK 跨语言字段不一致

**症状**:Go SDK / Python SDK / TS SDK / Rust SDK 调同一端点,JSON 字段大小写 / 顺序不同。

**原因**:JSON 序列化策略差异(Go json.Marshal / Python json.dumps / TS JSON.stringify / Rust serde_json)。

**修复**:
- **Stage 0 收口**(2026-06-28):4 SDK 5/5 字段对齐(per [[project-v0-9-0-stage0-closure-2026-06-28]])
- **Stage 3.1 #4-#7** 实测:4 SDK Chat completions 全部 2xx 响应,字段字节级对齐
- **基准**:`wau-go-sdk/types.go` 为准(per ADR-0004),其它 3 SDK 镜像对齐
- **小差异**:omitempty 字段顺序不影响语义,JSON parser 都容忍

---

### Q9: 流式响应 / SSE 不工作

**症状**:`Stream: true` 返回 nil 或非预期数据。

**原因**:v0.9.0 alpha **不支持 streaming**(per chat.go:16-18 注释)。

**修复**:
```go
// v0.9.0 alpha:用 Completions() non-streaming
resp, err := c.Chat().Completions(ctx, wau.ChatCompletionRequest{
    Model:    "wau-default",
    Messages: []wau.ChatMessage{{Role: "user", Content: "hi"}},
    Stream:   false, // ← 必须是 false
})

// v1.2.0+:用 CompletionsStream(ctx, req) (per §8.3 路线)
```

---

### Q10: TLS / CA 证书错误

**症状**:
```
x509: certificate signed by unknown authority
```

**原因**:自签证书 / CA bundle 缺失。

**修复**:
```go
// 1. 注入跳过验证的 HTTP client(仅 dev!)
httpClient := &http.Client{
    Transport: &http.Transport{
        TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
    },
    Timeout: 30 * time.Second,
}
c, _ := wau.New("https://wau.example.com",
    wau.WithHTTPClient(httpClient),
)

// 2. 或配系统 CA bundle
export SSL_CERT_FILE=/path/to/ca-bundle.crt

// 3. 生产:用正规 CA 签名证书(Let's Encrypt 等)
```

---

## Go 语言特定问题(5 Q)

### Q11: `go mod tidy` 网络问题

**症状**:
```
go: github.com/wau/wau-go-sdk@v1.1.0: verifying module: checksum mismatch
```
或
```
go: github.com/golang-jwt/jwt/v5@v5.2.1: unrecognized import path "golang.org/x/crypto"
```

**修复**:
```bash
# 1. 设置 GOPROXY 国内镜像
go env -w GOPROXY=https://goproxy.cn,direct
go env -w GOSUMDB=sum.golang.google.cn

# 2. 或禁用 sum 验证(仅 dev!)
go env -w GOSUMDB=off
go mod tidy

# 3. 私有仓认证
go env -w GOPRIVATE=github.com/wau
git config --global url."https://$TOKEN@github.com/".insteadOf "https://github.com/"
```

---

### Q12: import cycle

**症状**:
```
import cycle not allowed
package github.com/wau/wau-go-sdk/bot/telegram
    imports github.com/wau/wau-go-sdk
    imports github.com/wau/wau-go-sdk/bot/telegram
```

**原因**:Bot 子包跟主包互相 import。

**修复**:
```go
// 1. Bot 子包只 import 主包(单向,无 cycle)
package bottelegram // bot/telegram/telegram.go

import (
    wau "github.com/wau/wau-go-sdk"          // ✓ 单向
    "github.com/wau/wau-go-sdk/bot/common"  // ✓ 单向
)

// 2. 主包不要 import Bot 子包(用户自己 import)
//    (设计原则,per wau-go-sdk 仓布局)
```

---

### Q13: `errors.Is` 链断

**症状**:`errors.Is(err, wau.ErrNotFound)` 返 false,即使 server 返 404。

**原因**:`fmt.Errorf("...: %w", err)` 包装丢失 sentinel 类型,或 sentinel 用 `errors.New()` 没 `Is()`。

**修复**:
```go
// 1. APIError 实现 Is() (per errors.go:27-33)
func (e *APIError) Is(target error) bool {
    var t *APIError
    if !errors.As(target, &t) {
        return false
    }
    return e.StatusCode == t.StatusCode
}

// 2. 用户代码:errors.As 拆 APIError
resp, err := c.Chat().Completions(ctx, req)
if err != nil {
    var apiErr *wau.APIError
    if errors.As(err, &apiErr) {
        log.Printf("status=%d code=%s", apiErr.StatusCode, apiErr.Code)
    }
    // 或用 errors.Is:
    if errors.Is(err, wau.ErrNotFound) {
        // ...
    }
}

// 3. 自定义 sentinel 时,要实现 Is() 方法或用 errors.Join(Go 1.20+)
```

---

### Q14: `context.Canceled` 不返

**症状**:用户调 `cancel()`,但 goroutine 卡死不退出。

**原因**:没监听 `ctx.Done()`,只 sleep / blocking IO。

**修复**:
```go
// 1. goroutine 必须 select ctx.Done()
func (t *TelegramBot) handleUpdates(ctx context.Context) {
    for {
        select {
        case <-t.stopped:
            return
        case <-ctx.Done(): // ← 监听
            return
        case upd, ok := <-t.updates:
            if !ok { return }
            t.processMessage(ctx, upd.Message)
        }
    }
}

// 2. HTTP 请求用 NewRequestWithContext(ctx, ...)
httpReq, _ := http.NewRequestWithContext(ctx, "POST", url, body)
// (per chat.go:97)

// 3. 不要用 background context
ctx := context.Background() // ❌ 永远不取消
ctx, cancel := context.WithCancel(parentCtx) // ✓ 可取消
defer cancel()
```

---

### Q15: goroutine 泄漏 / pprof 看不出来

**症状**:`runtime.NumGoroutine()` 单调递增,bot 跑几天后内存爆。

**原因**:goroutine 没退出路径(per Q14),或 `sync.WaitGroup.Add/Done` 漏配。

**修复**:
```go
// 1. 用 pprof 抓 goroutine
import _ "net/http/pprof"

go func() {
    http.ListenAndServe(":6060", nil)
}()

// 访问 http://localhost:6060/debug/pprof/goroutine?debug=1
// 看堆栈找泄漏点

// 2. 配 sync.WaitGroup + defer Done
var wg sync.WaitGroup
wg.Add(1)
go func() {
    defer wg.Done()
    // ... 处理
}()
wg.Wait() // 主流程等

// 3. 用 context 控制生命周期(per Q14)

// 4. 测试:runtime.NumGoroutine() 前后对比
before := runtime.NumGoroutine()
// ... 启 bot,跑 1000 条消息,停 bot
after := runtime.NumGoroutine()
if after > before+1 { panic("goroutine leak") }
```

---

## 性能调优(预留,留给 v1.0.0 实测后)

> 本期(07-01)不写,留给 v1.0.0 实测后补。

- HTTP 连接池调优(`http.Transport.MaxIdleConns` / `MaxConnsPerHost` / `IdleConnTimeout`)
- TLS handshake 复用(`http.Transport.TLSHandshakeTimeout`)
- 并发场景下 `sync.Pool` 复用 `bytes.Buffer`(JSON 序列化)
- 长 timeout 任务的 streaming(留 v1.2.0)

---

## 链接

- [README.md](./README.md) — 入口
- [API.md](./API.md) — 完整 API 参考
- [QUICKSTART.md](./QUICKSTART.md) — 15 分钟跑通
- [CHANGELOG.md](./CHANGELOG.md) — 版本变更
- [docs/auth.md](./docs/auth.md) — HS256 + JWT 详细
- [docs/retry_circuit.md](./docs/retry_circuit.md) — 重试 + 熔断详细
- [examples/](./examples/) — 7 个 runnable example

---

**维护**:Claude + youhaoxi(Stage 3.2 SDK doc 完整化,2026-07-01)
**WAU 业务代码改动 = 0**(纯文档,不改 .go)
// Package wau — wau-go-sdk v0.6.0-preview.1 官方 Go SDK
//
// 抽取自 wau-cli/internal/client/(2026-06-13),扩展:
//   - typed errors (errors.Is(err, ErrNotFound))
//   - 熔断(翻译 wau-circuit) — W5.4 实装
//   - 重试(指数退避 + 抖动) — W5.4 实装
//   - HS256 鉴权 — W5.5 实装
//   - SubmitRequest 字段以 kernel 真相源为准
//
// 用法:
//
//	c, err := wau.New("http://localhost:18400",
//	    wau.WithTimeout(10*time.Second),
//	    wau.WithAuth(wau.AuthConfig{...}),
//	)
//	defer c.Close()
//
//	agents, err := c.Agents().List(ctx, wau.PageOptions{PageSize: 10})
//	resp, err := c.Tasks().Submit(ctx, wau.SubmitRequest{Prompt: "hello"})
package wau

import (
	"context"
	"log/slog"
	"net/http"
)

// Client is the top-level WAU SDK entry point.
//
// 它是 immutable 的;所有可变状态(熔断器、JWT signer)通过内部指针共享。
// 多个 goroutine 并发安全(transport 跟 circuit 都用 sync.RWMutex/sync.Mutex)。
type Client struct {
	baseURL string
	opts    Options
	tp      *transport
	rt      *retrier
	ca      *circuitAdapter
	signer  *signer
	agents  *AgentsService
	tasks   *TasksService
	kernel  *KernelService
	intent  *IntentService
	logger  *slog.Logger
}

// New creates a new WAU SDK client.
//
// baseURL 形如 "http://localhost:18400" 或 "https://wau.example.com"。
// 返回的 *Client 必须调用 Close() 释放资源(M3.1 阶段 gRPC client 才需要)。
func New(baseURL string, opts ...Option) (*Client, error) {
	o := applyOptions(opts)
	if baseURL == "" {
		baseURL = "http://localhost:18400"
	}
	c := &Client{
		baseURL: baseURL,
		opts:    o,
		tp:      newTransport(baseURL, o),
		rt:      newRetrier(o.Retry),
		ca:      newCircuitAdapter(o.Circuit, o.Logger),
		logger:  o.Logger,
	}
	// 鉴权可选
	if len(o.Auth.SharedSecret) > 0 {
		s, err := newSigner(o.Auth)
		if err != nil {
			return nil, err
		}
		c.signer = s
	}
	c.agents = &AgentsService{c: c}
	c.tasks = &TasksService{c: c}
	c.kernel = &KernelService{c: c}
	c.intent = &IntentService{c: c}
	return c, nil
}

// Agents returns the AgentsService for agent CRUD operations.
func (c *Client) Agents() *AgentsService { return c.agents }

// Tasks returns the TasksService for task submit / get / simulate.
func (c *Client) Tasks() *TasksService { return c.tasks }

// Kernel returns the KernelService for kernel info / health.
func (c *Client) Kernel() *KernelService { return c.kernel }

// Intent returns the IntentService (gRPC stub — M3.1 实装,目前返 ErrNotImplemented).
func (c *Client) Intent() *IntentService { return c.intent }

// Close releases SDK resources. Currently no-op; will be used by M3.1 gRPC client.
func (c *Client) Close() error { return nil }

// BaseURL returns the base URL (useful for tests / debug).
func (c *Client) BaseURL() string { return c.baseURL }

// CircuitState 返回当前 SDK 内部熔断状态(给 debug / metrics 用)
func (c *Client) CircuitState() string { return c.ca.State() }

// doWithRetry 在 transport.do 外面包重试 + 鉴权 + 熔断。
//
// 调用链:Caller → Client.doWithRetry → Retrier.Do → Circuit.Guard → Transport.do
//
// 错误返回:
//   - ErrCircuitOpen: 熔断开
//   - ErrMaxRetries: 重试耗尽
//   - *APIError: HTTP 4xx/5xx(可能经重试后仍是)
//   - context.Canceled / DeadlineExceeded: ctx 取消
func (c *Client) doWithRetry(ctx context.Context, method, path string, body, v any) error {
	op := func() error {
		return c.ca.Guard(ctx, func() error {
			// 鉴权签 JWT(每次请求新签)
			if c.signer != nil {
				jwtStr, err := c.signer.Sign()
				if err != nil {
					return err
				}
				// 用 transport 内置的 header 注入
				c.tp.setAuthHeader("Bearer " + jwtStr)
			}
			return c.tp.do(ctx, method, path, body, v)
		})
	}
	return c.rt.Do(ctx, op)
}

// 避免 import "net/http" 未用
var _ = http.MethodGet

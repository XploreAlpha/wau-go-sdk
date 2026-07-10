package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// Client 是 MCP client(对应 WAU-core-kernel internal/protocol/mcp.Server)。
//
// 用法:
//
//	cli, err := mcpclient.NewClient("https://kernel.example.com",
//	    mcpclient.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
//	    mcpclient.WithBearerToken(jwtStr),
//	)
//	if err != nil { ... }
//	defer cli.Close()
//
//	card, err := cli.ParseAgentCard(ctx, []byte(`{"name":"Fox"}`))
type Client struct {
	// baseURL kernel MCP server 入口,例 "https://kernel.example.com"
	// (不带 trailing slash,不带 /mcp path)。
	baseURL string

	// endpoint 完整 endpoint path,固定 "/mcp"(JSON-RPC 2.0 dispatcher)。
	endpoint string

	// httpClient 由 caller 注入(类似 wau-go-sdk Options.HTTPClient)。
	// 默认 nil → 用 http.DefaultClient。
	httpClient *http.Client

	// bearerToken 可选 JWT(D78/D79 拍板);空 = 不发 Authorization header。
	bearerToken string

	// userAgent 可选 User-Agent;空 = 默认 "wau-go-sdk/mcpclient/v1.3.2"。
	userAgent string

	// nextID 自增 request ID(per JSON-RPC 2.0 spec 可用 number)。
	// 0 是初始值,从 1 开始。
	nextID atomic.Int64
}

// Option 是 NewClient 的 functional option。
type Option func(*Client)

// WithHTTPClient 注入自定义 *http.Client(测试用 httptest 或自定义 TLS)。
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithBearerToken 注入 JWT bearer token(D78/D79 拍板,W2.0 closure)。
//
// 空字符串 = 不发 Authorization header(允许匿名访问 MCP,视 server 端策略而定)。
func WithBearerToken(token string) Option {
	return func(c *Client) { c.bearerToken = token }
}

// WithUserAgent 覆盖默认 User-Agent。
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// WithEndpoint 覆盖默认 endpoint path("/mcp"),主要用于测试。
func WithEndpoint(path string) Option {
	return func(c *Client) { c.endpoint = path }
}

// NewClient 构造 MCP client。
//
// baseURL 例 "https://kernel.example.com" 或 "http://localhost:8443"
// (不带 trailing slash,不带 /mcp)。
func NewClient(baseURL string, opts ...Option) (*Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("mcpclient: baseURL is required")
	}
	c := &Client{
		baseURL:    baseURL,
		endpoint:   "/mcp",
		userAgent:  "wau-go-sdk/mcpclient/v1.3.2",
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	// baseURL 末尾 slash 去掉(避免双 slash)
	if c.baseURL[len(c.baseURL)-1] == '/' {
		c.baseURL = c.baseURL[:len(c.baseURL)-1]
	}
	return c, nil
}

// Close 释放资源(stub:目前无连接池,留口给 future SSE long-lived stream)。
func (c *Client) Close() error {
	return nil
}

// ────────────────────────────────────────────────────────
// JSON-RPC 2.0 主入口
// ────────────────────────────────────────────────────────

// call 是 SDK 内部用的 typed-agnostic JSON-RPC 调用入口。
//
// 流程:
//  1. 构造 envelope {jsonrpc, id, method, params}
//  2. POST baseURL+endpoint
//  3. 解析 Response envelope
//  4. if error → 返 *RPCError;else 解析 result 到 v
//
// notification == true 时:不发 id 字段(server 返 204 No Content,不读 body)。
func (c *Client) call(ctx context.Context, method string, params map[string]any, notification bool, v any) error {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	if !notification {
		req.ID = c.nextID.Add(1)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("mcpclient: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mcpclient: build http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", c.userAgent)
	if c.bearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("mcpclient: http do: %w", err)
	}
	defer resp.Body.Close()

	// Notification:server 返 204 No Content,不读 body
	if notification {
		if resp.StatusCode == http.StatusNoContent {
			return nil
		}
		// 其它状态码仍然要读 body 拿到错误信息
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("mcpclient: read response body: %w", err)
	}

	// 4xx/5xx → 期望仍是 JSON-RPC envelope(kernel server 总是返 200 + envelope),
	// 但其他实现可能走 REST 错误,这里给 fallback
	if resp.StatusCode >= 400 {
		return &RPCError{
			Code:    resp.StatusCode * -1, // 转负数跟 spec code 区分
			Message: fmt.Sprintf("http %d: %s", resp.StatusCode, string(respBody)),
		}
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return fmt.Errorf("mcpclient: unmarshal response (status=%d): %w (body=%s)",
			resp.StatusCode, err, string(respBody))
	}

	if rpcResp.Error != nil {
		return rpcResp.Error // 已是 *RPCError
	}

	if v != nil && rpcResp.Result != nil {
		if err := json.Unmarshal(rpcResp.Result, v); err != nil {
			return fmt.Errorf("mcpclient: unmarshal result: %w", err)
		}
	}
	return nil
}

// jsonRPCRequest 是 JSON-RPC 2.0 request envelope。
//
// 用 map 序列化(per spec,params 可以是 object 或 array),
// 这里统一用 object(对齐 MCP convention)。
type jsonRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
	ID      int64          `json:"id,omitempty"` // notification 时省略
}

// jsonRPCResponse 是 JSON-RPC 2.0 response envelope。
//
// Result 用 json.RawMessage 因为类型依 method 而定(由 caller 提供 v 解析)。
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      int64           `json:"id"`
}

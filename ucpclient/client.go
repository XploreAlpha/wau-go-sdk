// Package ucpclient provides a UCP client for WAU.
//
// ⭐ v1.0.0 M11 P7 UCP client (per D88.5,2026-07-11)。
//
// 5 SDK 共享 wire format:JSON-RPC 2.0 over HTTP at POST {baseURL}/ucp
// (跟 WAU-core-kernel internal/protocol/ucp/server.go handleUCP 对齐)。
//
// 本子包 = 11 commerce tool wrapper (ListProducts / GetProduct /
// SearchProducts / AddToCart / GetCart / RemoveFromCart /
// CreateCheckoutSession / ConfirmPayment / GetOrder / ListOrders /
// CancelOrder) + 8 commerce DTO + JSON-RPC envelope + error types。
//
// 协议合规:
//   - D60 additive: 0 改老 SDK,独立子包(mcpclient/ 已有,v1.3.2 → v1.3.3 additive)
//   - D13 byte-equal: JSON wire format 5 SDK 一致(per design doc §三)
//   - D65 (tenant_id): Order / Cart DTO 含 tenant_id 字段
//   - D66=B RBAC: owner_user_id 维持 string
//   - D78/D79/D80: UCP OAuth 2.0 identity_linking bearer token,跟 MCP JWT 走同一通道
//   - D88 ⭐⭐: 本子包 = D88.5 Go SDK UCP client 实装(W3-launch-SOP §3.3 拍板)
//
// 设计原则(跟 mcpclient/ 1:1):
//   - 不引入 testify / gomock,用 net/http + stdlib
//   - HTTPClient 由 caller 注入(同 Options.HTTPClient pattern),便于测试 httptest
//   - 错误统一返 *RPCError,5 spec code + 5 UCP code 跟 kernel ucp.Envelope 一致
package ucpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// Client 是 UCP client(对应 WAU-core-kernel internal/protocol/ucp.Server)。
//
// 用法:
//
//	cli, err := ucpclient.NewClient("https://kernel.example.com",
//	    ucpclient.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
//	    ucpclient.WithBearerToken(oauthJWT),
//	)
//	if err != nil { ... }
//	defer cli.Close()
//
//	cart, err := cli.AddToCart(ctx, "prod-123", 2)
type Client struct {
	// baseURL kernel UCP server 入口,例 "https://kernel.example.com"
	// (不带 trailing slash,不带 /ucp path)。
	baseURL string

	// endpoint 完整 endpoint path,固定 "/ucp"(JSON-RPC 2.0 dispatcher)。
	endpoint string

	// httpClient 由 caller 注入(类似 wau-go-sdk Options.HTTPClient)。
	// 默认 nil → 用 http.DefaultClient。
	httpClient *http.Client

	// bearerToken 可选 OAuth 2.0 identity_linking JWT(D78/D79 拍板 + UCP spec);
	// 空 = 不发 Authorization header。
	bearerToken string

	// userAgent 可选 User-Agent;空 = 默认 "wau-go-sdk/ucpclient/v1.3.3"。
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

// WithBearerToken 注入 OAuth 2.0 identity_linking JWT bearer token
// (D78/D79 拍板,W2.0 closure)。
//
// 空字符串 = 不发 Authorization header(允许匿名访问 UCP,视 server 端策略而定)。
func WithBearerToken(token string) Option {
	return func(c *Client) { c.bearerToken = token }
}

// WithUserAgent 覆盖默认 User-Agent。
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// WithEndpoint 覆盖默认 endpoint path("/ucp"),主要用于测试。
func WithEndpoint(path string) Option {
	return func(c *Client) { c.endpoint = path }
}

// NewClient 构造 UCP client。
//
// baseURL 例 "https://kernel.example.com" 或 "http://localhost:8444"
// (不带 trailing slash,不带 /ucp)。
func NewClient(baseURL string, opts ...Option) (*Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("ucpclient: baseURL is required")
	}
	c := &Client{
		baseURL:    baseURL,
		endpoint:   "/ucp",
		userAgent:  "wau-go-sdk/ucpclient/v1.3.3",
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
// JSON-RPC 2.0 主入口(callTool 通用 dispatch)
// ────────────────────────────────────────────────────────

// callTool 是 SDK 内部用的 typed-agnostic JSON-RPC 调用入口。
//
// 流程:
//  1. 构造 envelope {jsonrpc, id, method, params: {name, arguments}}
//  2. POST baseURL+endpoint
//  3. 解析 Response envelope
//  4. if error → 返 *RPCError;else 解析 result 到 out
//
// notification == true 时:不发 id 字段(server 返 204 No Content,不读 body)。
// UCP 11 tool 全部 sync,无 notification 用法,这里保留参数是为未来 SSE 兼容。
func (c *Client) callTool(ctx context.Context, toolName string, arguments any, out any) error {
	params := map[string]any{
		"name":      toolName,
		"arguments": arguments,
	}
	// arguments 为 nil 时省略(omitempty-like)
	if arguments == nil {
		delete(params, "arguments")
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params:  params,
		ID:      c.nextID.Add(1),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("ucpclient: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ucpclient: build http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", c.userAgent)
	if c.bearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ucpclient: http do: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("ucpclient: read response body: %w", err)
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
		return fmt.Errorf("ucpclient: unmarshal response (status=%d): %w (body=%s)",
			resp.StatusCode, err, string(respBody))
	}

	if rpcResp.Error != nil {
		return rpcResp.Error // 已是 *RPCError
	}

	if out != nil && rpcResp.Result != nil {
		if err := json.Unmarshal(rpcResp.Result, out); err != nil {
			return fmt.Errorf("ucpclient: unmarshal result: %w", err)
		}
	}
	return nil
}

// jsonRPCRequest 是 JSON-RPC 2.0 request envelope。
//
// 用 map 序列化(per spec,params 可以是 object 或 array),
// 这里统一用 object(对齐 UCP convention,named params)。
type jsonRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
	ID      int64          `json:"id,omitempty"` // notification 时省略
}

// jsonRPCResponse 是 JSON-RPC 2.0 response envelope。
//
// Result 用 json.RawMessage 因为类型依 method 而定(由 caller 提供 out 解析)。
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      int64           `json:"id"`
}

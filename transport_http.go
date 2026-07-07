package wau

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// transport 是 SDK 的 HTTP 传输层。
//
// 设计原则:
//   - 不重试:重试逻辑在 Client.doWithRetry
//   - 不鉴权:JWT 由 Client 注入
//   - 不熔断:熔断由 Client.ca.Guard
//
// 作用:把 ctx + method + path + body 拼成 HTTP 请求,解码 JSON 响应,
// 4xx/5xx 翻译成 *APIError。
type transport struct {
	baseURL    string
	httpClient *http.Client
	userAgent  string

	mu        sync.RWMutex
	authValue string // Bearer token (动态注入)
}

func newTransport(baseURL string, opts Options) *transport {
	return &transport{
		baseURL:    baseURL,
		httpClient: opts.HTTPClient,
		userAgent:  opts.UserAgent,
	}
}

// setAuthHeader 动态注入 Authorization header (Bearer JWT).
// 由 Client.doWithRetry 在每次请求前调。
func (t *transport) setAuthHeader(v string) {
	t.mu.Lock()
	t.authValue = v
	t.mu.Unlock()
}

// authHeader 返回当前注入的 Authorization header 值(无则返空字符串)。
//
// 用于绕过 doWithRetry 直接发请求的代码路径(如 v1.0.0 M11 P4
// SkillsService.Publish 的 multipart upload)。
func (t *transport) authHeader() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.authValue
}

// do 执行一次 HTTP 请求。
//
// 错误返回:
//   - *APIError: HTTP 4xx/5xx
//   - 其它: 网络错 / JSON 解码错
func (t *transport) do(ctx context.Context, method, path string, body, v any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("wau: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, t.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("wau: new request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", t.userAgent)

	// 鉴权 header(动态)
	t.mu.RLock()
	auth := t.authValue
	t.mu.RUnlock()
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("wau: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("wau: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return &APIError{
			StatusCode: resp.StatusCode,
			RequestID:  resp.Header.Get("X-Request-ID"),
			Body:       respBody,
		}
	}

	if v != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, v); err != nil {
			return fmt.Errorf("wau: decode response: %w", err)
		}
	}
	return nil
}

// Get is a convenience wrapper.
func (t *transport) Get(ctx context.Context, path string, v any) error {
	return t.do(ctx, http.MethodGet, path, nil, v)
}

// Post is a convenience wrapper.
func (t *transport) Post(ctx context.Context, path string, body, v any) error {
	return t.do(ctx, http.MethodPost, path, body, v)
}

// Put is a convenience wrapper.
func (t *transport) Put(ctx context.Context, path string, body, v any) error {
	return t.do(ctx, http.MethodPut, path, body, v)
}

// Delete is a convenience wrapper.
func (t *transport) Delete(ctx context.Context, path string, v any) error {
	return t.do(ctx, http.MethodDelete, path, nil, v)
}

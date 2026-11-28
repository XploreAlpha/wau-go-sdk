package wau

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	// callerAgent 调用方 agent 名(声明"我是我")。
	//
	// ★ 2026-11-28:kernel 的 L4 派活会剔除调用方自己,防自派活死循环
	// (事故:烧掉 $14,822 / 3470 次调用)。见 Options.CallerAgent。
	callerAgent string

	mu        sync.RWMutex
	authValue string // Bearer token (动态注入)
}

func newTransport(baseURL string, opts Options) *transport {
	// ★ 2026-11-28:显式 CallerAgent 优先,否则回落到 Auth.AgentName
	// (HS256 场景不用重复配)。
	caller := opts.CallerAgent
	if caller == "" {
		caller = opts.Auth.AgentName
	}
	return &transport{
		baseURL:     baseURL,
		httpClient:  opts.HTTPClient,
		userAgent:   opts.UserAgent,
		callerAgent: caller,
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
//
// ★ 2026-11-19:path 支持**绝对 URL**(http://… / https://…)⇒ 直接用,不改 host。
// A2A 客户端要连**别的** agent 的地址,不是 kernel。
func (t *transport) do(ctx context.Context, method, path string, body, v any) error {
	return t.doWithHeaders(ctx, method, path, body, v, nil)
}

// doWithHeaders 执行一次 HTTP 请求,并附加自定义 header。
//
// ★ 2026-11-19 新增:A2A 客户端要带 `A2A-Version: 1.0` 头
// (缺失或值不匹配服务端直接拒),且 Authorization 要用 agent 自己的
// peer token 覆盖 kernel 的 JWT。
func (t *transport) doWithHeaders(ctx context.Context, method, path string, body, v any, extra map[string]string) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("wau: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	// ★ 2026-11-19:绝对 URL 直接用(不拼 baseURL)
	url := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		url = t.baseURL + path
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("wau: new request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", t.userAgent)

	// ★ 2026-11-28:声明"调用方是我" ⇒ kernel 把我从候选里剔除。
	//
	// ⚠️ 这个头**只做减法**(把自己踢出候选),不做加法(选别人)
	// ⇒ 客户端谎报的收益是零(只会让自己不被选中)。
	if t.callerAgent != "" {
		req.Header.Set("X-WAU-Caller-Agent", t.callerAgent)
	}

	// 鉴权 header(动态)
	t.mu.RLock()
	auth := t.authValue
	t.mu.RUnlock()
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}

	// ★ 2026-11-19:自定义 header 覆盖默认值
	// (A2A 的 Authorization 要用 agent 的 peer token,不是 kernel 的 JWT)
	for k, val := range extra {
		req.Header.Set(k, val)
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

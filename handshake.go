// v0.8.0 M5-1 B.1 — wau-go-sdk Handshake client
//
// 对应 kernel 端点(per WAU-core-kernel/cmd/wau-core/handle_handshake.go):
//   - POST /v0.8.0/handshake/sessions
//   - GET  /v0.8.0/handshake/sessions/{session_id}?tenant_id=xxx
//   - GET  /admin/handshake/stats
//
// 沿用 wau-go-sdk 现有 service 模式(per agents.go:18-30 AgentsService):
//   - 持有 c *Client
//   - 走 c.doWithRetry(ctx, method, path, body, &resp) 一行封装
//   - 自动应用: HS256 鉴权 + 熔断 + 重试
//
// 错误处理:用 *APIError + 9 个 handshake 哨兵(per plan §A.2):
//   -32001 InsufficientTrust   → ErrHandshakeInsufficientTrust
//   -32002 AgentNotFound       → ErrHandshakeAgentNotFound
//   -32003 TenantMismatch      → ErrHandshakeTenantMismatch
//   -32004 RateLimited         → ErrHandshakeRateLimited
//   -32005 ProtocolNotSupported → ErrHandshakeProtocolNotSupported
//   SESSION_NOT_FOUND          → ErrHandshakeSessionNotFound
//   AGENT_NO_ENDPOINT          → ErrHandshakeAgentNoEndpoint
//   INVALID_PROTOCOL           → ErrHandshakeInvalidProtocol
//   -32600 InvalidRequest      → ErrHandshakeInvalidRequest
//
// DTO 字段 1:1 对齐 kernel internal/handshake/session.go:92-142。
package wau

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// HandshakeService 提供 handshake 操作(创建/查询/统计)。
//
// 用法:
//
//	c, _ := wau.New("http://localhost:18400")
//	resp, err := c.Handshake().CreateSession(ctx, wau.HandshakeRequest{
//	    TenantID: "tenant-A",
//	    AgentID:  "Benny",
//	    Protocol: "a2a",
//	})
type HandshakeService struct {
	c *Client
}

// HandshakeRequest POST /v0.8.0/handshake/sessions 请求体。
//
// 必填: TenantID, AgentID(ClientID 不填时自动用 SDK user_agent)
// 可选: Protocol(默认 "a2a"), Universe
type HandshakeRequest struct {
	TenantID string `json:"tenant_id"`
	ClientID string `json:"client_id,omitempty"`
	AgentID  string `json:"agent_id"`
	Protocol string `json:"protocol,omitempty"`
	Universe string `json:"universe,omitempty"`
}

// HandshakeResponse POST /v0.8.0/handshake/sessions 响应(6 字段)。
//
// 复用判断: Reused=true 表示 kernel 命中了已存在的 session;
// 客户端可以接着用 SessionID 直连 DirectEndpoint(per D2 拍板:返 endpoint 不返 token)。
type HandshakeResponse struct {
	SessionID      string `json:"session_id"`
	DirectEndpoint string `json:"direct_endpoint"`
	Protocol       string `json:"protocol"`
	ExpiresAt      string `json:"expires_at"` // RFC3339
	TTLSeconds     int    `json:"ttl_seconds"`
	Reused         bool   `json:"reused"`
}

// HandshakeSessionDetail GET /v0.8.0/handshake/sessions/{id} 响应(11 字段)。
//
// 包含 trust_score + reuse_count 等 POST 不返的元信息,SDK 用这个端点
// ping session 状态或查 trust 分数。
type HandshakeSessionDetail struct {
	SessionID      string  `json:"session_id"`
	TenantID       string  `json:"tenant_id"`
	ClientID       string  `json:"client_id"`
	AgentID        string  `json:"agent_id"`
	DirectEndpoint string  `json:"direct_endpoint"`
	Protocol       string  `json:"protocol"`
	TrustScore     float64 `json:"trust_score"`
	CreatedAt      string  `json:"created_at"`
	ExpiresAt      string  `json:"expires_at"`
	TTLSeconds     int     `json:"ttl_seconds"`
	ReuseCount     int     `json:"reuse_count"`
}

// CreateSession POST /v0.8.0/handshake/sessions
//
// 必传 TenantID + AgentID;ClientID 不传时自动用 SDK user_agent(如 "wau-go-sdk/1.1.0")。
//
// 错误返回: 9 个握手哨兵之一(ErrHandshakeAgentNotFound / ErrHandshakeTenantMismatch 等),
// 用 errors.Is(err, ErrHandshakeAgentNotFound) 判断。
func (s *HandshakeService) CreateSession(ctx context.Context, req HandshakeRequest) (*HandshakeResponse, error) {
	if req.ClientID == "" {
		// 自动填 client_id = SDK user_agent(如 "wau-go-sdk/1.1.0")
		// per transport_http.go:35 注入路径
		req.ClientID = s.c.opts.UserAgent
	}
	var resp HandshakeResponse
	if err := s.c.doWithRetry(ctx, http.MethodPost, "/v0.8.0/handshake/sessions", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetSession GET /v0.8.0/handshake/sessions/{session_id}?tenant_id=xxx
//
// tenantID 必须传,用于跨 tenant 防护(kernel 会判 tenant ownership)。
func (s *HandshakeService) GetSession(ctx context.Context, sessionID, tenantID string) (*HandshakeSessionDetail, error) {
	if tenantID == "" {
		return nil, errors.New("wau: tenant_id is required for GetSession")
	}
	path := fmt.Sprintf("/v0.8.0/handshake/sessions/%s?tenant_id=%s", sessionID, tenantID)
	var resp HandshakeSessionDetail
	if err := s.c.doWithRetry(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetStats GET /admin/handshake/stats
//
// 返回 hit rate 监控数据(per kernel handshake.StatsResponse)。
type HandshakeStats struct {
	TotalSessions  int64                  `json:"total_sessions"`
	TotalReuses    int64                  `json:"total_reuses"`
	ReuseHitRate   float64                `json:"reuse_hit_rate"`
	ActiveSessions int64                  `json:"active_sessions"`
	PerTenant      map[string]TenantStats `json:"per_tenant,omitempty"`
}

type TenantStats struct {
	Sessions int64   `json:"sessions"`
	Reuses   int64   `json:"reuses"`
	HitRate  float64 `json:"hit_rate"`
}

func (s *HandshakeService) GetStats(ctx context.Context) (*HandshakeStats, error) {
	var resp HandshakeStats
	if err := s.c.doWithRetry(ctx, http.MethodGet, "/admin/handshake/stats", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ============== 9 个握手哨兵(per plan §A.2)==============
//
// 用 errors.Is(err, ErrHandshakeAgentNotFound) 等判。
// 每个哨兵带 Code(数字或字符串) + StatusCode(HTTP),复用 APIError 的 Is() 逻辑。

var (
	ErrHandshakeInsufficientTrust = &APIError{StatusCode: 403, Code: "INSUFFICIENT_TRUST", Message: "client trust score below 0.5 threshold"}
	ErrHandshakeAgentNotFound     = &APIError{StatusCode: 404, Code: "AGENT_NOT_FOUND", Message: "agent not found in registry"}
	ErrHandshakeTenantMismatch    = &APIError{StatusCode: 403, Code: "TENANT_MISMATCH", Message: "tenant does not own this session"}
	ErrHandshakeRateLimited       = &APIError{StatusCode: 429, Code: "RATE_LIMITED", Message: "rate limit exceeded (100 req/min per client)"}
	ErrHandshakeProtocolNotSupported = &APIError{StatusCode: 400, Code: "PROTOCOL_NOT_SUPPORTED", Message: "agent does not support requested protocol"}
	ErrHandshakeSessionNotFound   = &APIError{StatusCode: 404, Code: "SESSION_NOT_FOUND", Message: "session not found or expired"}
	ErrHandshakeAgentNoEndpoint   = &APIError{StatusCode: 404, Code: "AGENT_NO_ENDPOINT", Message: "agent has no endpoint"}
	ErrHandshakeInvalidProtocol   = &APIError{StatusCode: 400, Code: "INVALID_PROTOCOL", Message: "protocol not in ProtocolRegistry"}
	ErrHandshakeInvalidRequest    = &APIError{StatusCode: 400, Code: "INVALID_REQUEST", Message: "invalid request"}
)

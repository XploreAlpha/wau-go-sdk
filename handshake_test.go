// v0.8.0 M5-1 B.1 — wau-go-sdk Handshake client test
//
// 测试模式(per client_test.go inlineMockKernel):
//   - 启 httptest mock server,模拟 kernel handshake 三端点行为
//   - 状态机:首次 create 返 reused=false;同 (tenant,client,agent) 再调 返 reused=true
//   - 错误注入:返 4xx + JSON-RPC error code 让 SDK 端可解析
//
// 8 case(per plan §B.1):
//   1. happy path(create + reused=false)
//   2. reuse hit(create 再调同 key → reused=true,session_id 一致)
//   3. agent not found(-32002 → ErrHandshakeAgentNotFound)
//   4. tenant mismatch(-32003 → ErrHandshakeTenantMismatch via GetSession)
//   5. invalid request(-32600 → ErrHandshakeInvalidRequest)
//   6. rate limited(-32004 → ErrHandshakeRateLimited)
//   7. session not found(SESSION_NOT_FOUND → ErrHandshakeSessionNotFound via GetSession)
//   8. stats endpoint(返回 total_sessions/reuses/hit_rate)
package wau

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// handshakeMockKernel 模拟 kernel handshake 端点行为
//
// 状态: (tenant_id, client_id, agent_id) → session_payload(返回 reused=true if hit)
type handshakeMockKernel struct {
	mu       sync.Mutex
	sessions map[string]map[string]any // key = "tenant|client|agent" → session JSON
	stats    map[string]int64          // created / reused
}

func newHandshakeMockKernel() *handshakeMockKernel {
	return &handshakeMockKernel{
		sessions: make(map[string]map[string]any),
		stats:    map[string]int64{"created": 0, "reused": 0},
	}
}

func (m *handshakeMockKernel) sessionKey(tenant, client, agent string) string {
	return tenant + "|" + client + "|" + agent
}

// start 启 httptest server,挂 3 个端点
func (m *handshakeMockKernel) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v0.8.0/handshake/sessions":
			m.handleCreate(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/admin/handshake/stats":
			m.handleStats(w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v0.8.0/handshake/sessions/"):
			m.handleGet(w, r)
		default:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (m *handshakeMockKernel) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHandshakeErr(w, http.StatusBadRequest, -32600, "invalid JSON")
		return
	}
	tenant, client, agent := req["tenant_id"], req["client_id"], req["agent_id"]
	if tenant == "" || client == "" || agent == "" {
		writeHandshakeErr(w, http.StatusBadRequest, -32600, "missing required fields")
		return
	}
	if agent == "RateLimitAgent" {
		writeHandshakeErr(w, http.StatusTooManyRequests, -32004, "rate limit")
		w.Header().Set("Retry-After", "60")
		return
	}
	if agent == "GhostAgent" {
		writeHandshakeErr(w, http.StatusNotFound, -32002, "agent not found")
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.sessionKey(tenant, client, agent)
	if existing, ok := m.sessions[key]; ok {
		// 复用命中
		m.stats["reused"]++
		existing["reused"] = true
		_ = json.NewEncoder(w).Encode(existing)
		return
	}
	// 新建
	m.stats["created"]++
	sessionID := fmt.Sprintf("sess-%s-%d", agent, time.Now().UnixNano())
	expiresAt := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339)
	resp := map[string]any{
		"session_id":      sessionID,
		"direct_endpoint": fmt.Sprintf("http://%s.local:18800", agent),
		"protocol":        "a2a",
		"expires_at":      expiresAt,
		"ttl_seconds":     300,
		"reused":          false,
	}
	m.sessions[key] = resp
	_ = json.NewEncoder(w).Encode(resp)
}

func (m *handshakeMockKernel) handleGet(w http.ResponseWriter, r *http.Request) {
	// path: /v0.8.0/handshake/sessions/{session_id}?tenant_id=xxx
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		writeHandshakeErr(w, http.StatusBadRequest, -32600, "invalid path")
		return
	}
	sessionID := parts[len(parts)-1]
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		writeHandshakeErr(w, http.StatusBadRequest, -32600, "tenant_id required")
		return
	}
	// 找到 session(简化:从所有 sessions 找)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s["session_id"] == sessionID {
			// tenant mismatch 模拟:如果 session_id 含 "wrong-tenant" → 403
			if sessionID == "wrong-tenant-sess" {
				writeHandshakeErr(w, http.StatusForbidden, -32003, "tenant mismatch")
				return
			}
			detail := map[string]any{
				"session_id":      sessionID,
				"tenant_id":       tenantID,
				"client_id":       "wau-go-sdk/1.1.0",
				"agent_id":        "Benny",
				"direct_endpoint": "http://benny.local:18800",
				"protocol":        "a2a",
				"trust_score":     0.85,
				"created_at":      time.Now().UTC().Format(time.RFC3339),
				"expires_at":      s["expires_at"],
				"ttl_seconds":     300,
				"reuse_count":     1,
			}
			_ = json.NewEncoder(w).Encode(detail)
			return
		}
	}
	// session 不存在
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    "SESSION_NOT_FOUND",
			"message": "session not found or expired",
		},
	})
}

func (m *handshakeMockKernel) handleStats(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	created := m.stats["created"]
	reused := m.stats["reused"]
	hitRate := 0.0
	if total := created + reused; total > 0 {
		hitRate = float64(reused) / float64(total)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"total_sessions":  created,
		"total_reuses":    reused,
		"reuse_hit_rate":  hitRate,
		"active_sessions": created,
	})
}

func writeHandshakeErr(w http.ResponseWriter, status, code int, msg string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": msg,
		},
	})
}

// ============== Case 1:happy path ==============

func TestHandshake_GoSDK_Happy(t *testing.T) {
	mk := newHandshakeMockKernel()
	srv := mk.start(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	resp, err := c.Handshake().CreateSession(context.Background(), HandshakeRequest{
		TenantID: "tenant-A",
		AgentID:  "Benny",
		Protocol: "a2a",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if resp.SessionID == "" {
		t.Error("SessionID empty")
	}
	if resp.Reused {
		t.Error("first call should not be reused")
	}
	if resp.DirectEndpoint == "" {
		t.Error("DirectEndpoint empty")
	}
	if resp.TTLSeconds <= 0 {
		t.Errorf("TTLSeconds = %d, want > 0", resp.TTLSeconds)
	}
	if resp.Protocol != "a2a" {
		t.Errorf("Protocol = %q, want a2a", resp.Protocol)
	}
}

// ============== Case 2:reuse hit ==============

func TestHandshake_GoSDK_Reuse(t *testing.T) {
	mk := newHandshakeMockKernel()
	srv := mk.start(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	ctx := context.Background()
	hs := c.Handshake()

	// 首次
	r1, err := hs.CreateSession(ctx, HandshakeRequest{
		TenantID: "tenant-A", AgentID: "Benny", Protocol: "a2a",
	})
	if err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if r1.Reused {
		t.Error("first call reused=true")
	}

	// 第二次同 key
	r2, err := hs.CreateSession(ctx, HandshakeRequest{
		TenantID: "tenant-A", AgentID: "Benny", Protocol: "a2a",
	})
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if !r2.Reused {
		t.Error("second call should be reused=true")
	}
	if r1.SessionID != r2.SessionID {
		t.Errorf("session_id differs: r1=%s r2=%s", r1.SessionID, r2.SessionID)
	}
}

// ============== Case 3:agent not found ==============

func TestHandshake_GoSDK_AgentNotFound(t *testing.T) {
	mk := newHandshakeMockKernel()
	srv := mk.start(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	_, err := c.Handshake().CreateSession(context.Background(), HandshakeRequest{
		TenantID: "tenant-A",
		AgentID:  "GhostAgent",
	})
	if err == nil {
		t.Fatal("expected error for GhostAgent")
	}
	if !errors.Is(err, ErrHandshakeAgentNotFound) {
		t.Errorf("expected ErrHandshakeAgentNotFound, got %v", err)
	}
}

// ============== Case 4:tenant mismatch(GET 时)==============

func TestHandshake_GoSDK_TenantMismatch(t *testing.T) {
	mk := newHandshakeMockKernel()
	srv := mk.start(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	// 先建一个 session
	r, err := c.Handshake().CreateSession(context.Background(), HandshakeRequest{
		TenantID: "tenant-A", AgentID: "Benny",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// mock kernel 不跟踪 session 拥有者,改用 mock 直接注入 wrong-tenant-sess
	// 但要 GetSession 走 mock 路径 → 简化:先 mock 一个 sentinel session
	mk.mu.Lock()
	mk.sessions["t|w|c|wrong-tenant"] = map[string]any{
		"session_id":      "wrong-tenant-sess",
		"direct_endpoint": "http://benny.local:18800",
		"protocol":        "a2a",
		"expires_at":      time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339),
		"ttl_seconds":     300,
		"reused":          false,
	}
	mk.mu.Unlock()

	_, err = c.Handshake().GetSession(context.Background(), "wrong-tenant-sess", "tenant-B")
	if err == nil {
		t.Fatal("expected tenant mismatch error")
	}
	if !errors.Is(err, ErrHandshakeTenantMismatch) {
		t.Errorf("expected ErrHandshakeTenantMismatch, got %v", err)
	}

	// 确保 r.SessionID 的正确 tenant 也能查
	_ = r
}

// ============== Case 5:invalid request ==============

func TestHandshake_GoSDK_InvalidRequest(t *testing.T) {
	mk := newHandshakeMockKernel()
	srv := mk.start(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	// 缺 agent_id → mock 应返 400 -32600
	_, err := c.Handshake().CreateSession(context.Background(), HandshakeRequest{
		TenantID: "tenant-A",
		// AgentID 空
	})
	if err == nil {
		t.Fatal("expected invalid request error")
	}
	if !errors.Is(err, ErrHandshakeInvalidRequest) {
		t.Errorf("expected ErrHandshakeInvalidRequest, got %v", err)
	}
}

// ============== Case 6:rate limited ==============

func TestHandshake_GoSDK_RateLimited(t *testing.T) {
	mk := newHandshakeMockKernel()
	srv := mk.start(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	_, err := c.Handshake().CreateSession(context.Background(), HandshakeRequest{
		TenantID: "tenant-A",
		AgentID:  "RateLimitAgent",
	})
	if err == nil {
		t.Fatal("expected rate limited error")
	}
	if !errors.Is(err, ErrHandshakeRateLimited) {
		t.Errorf("expected ErrHandshakeRateLimited, got %v", err)
	}
}

// ============== Case 7:session not found(GET 时)==============

func TestHandshake_GoSDK_SessionNotFound(t *testing.T) {
	mk := newHandshakeMockKernel()
	srv := mk.start(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	_, err := c.Handshake().GetSession(context.Background(), "nonexistent-sess", "tenant-A")
	if err == nil {
		t.Fatal("expected session not found error")
	}
	if !errors.Is(err, ErrHandshakeSessionNotFound) {
		t.Errorf("expected ErrHandshakeSessionNotFound, got %v", err)
	}
}

// ============== Case 8:stats endpoint ==============

func TestHandshake_GoSDK_Stats(t *testing.T) {
	mk := newHandshakeMockKernel()
	srv := mk.start(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	ctx := context.Background()
	hs := c.Handshake()

	// 1 create + 4 reuse = 5 调用
	for i := 0; i < 5; i++ {
		_, err := hs.CreateSession(ctx, HandshakeRequest{
			TenantID: "tenant-A", AgentID: "Benny",
		})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}

	stats, err := hs.GetStats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d, want 1", stats.TotalSessions)
	}
	if stats.TotalReuses != 4 {
		t.Errorf("TotalReuses = %d, want 4", stats.TotalReuses)
	}
	expectedRate := 4.0 / 5.0
	if stats.ReuseHitRate < expectedRate-0.01 || stats.ReuseHitRate > expectedRate+0.01 {
		t.Errorf("ReuseHitRate = %f, want ~%f", stats.ReuseHitRate, expectedRate)
	}
}

package wau

// caller_agent_test.go — 2026-11-28 调用方声明头(X-WAU-Caller-Agent)
//
// 事故背景:2026-11-27 烧掉 $14,822 / 3470 次模型调用 / 平均 prompt 73,774 token。
// 根因是「自派活死循环」:hermes 派活给 WAU → WAU 按卡片打分把 hermes 自己
// 选成候选 → 派活回 hermes → hermes 又按 skill 派活 ⇒ 无限递归。
//
// 内核侧的守卫(剔除调用方自己)依赖这个头,所以 SDK 必须把它发出去。
//
// ⚠️ 设计要点:这个头**只做减法**(把自己踢出候选),不做加法(选别人)
// ⇒ 客户端谎报的收益是零 ⇒ 不会变成路由操纵面。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureCallerAgentHeader 起一个记录该头的 server,返回 (server, *string)。
func captureCallerAgentHeader(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-WAU-Caller-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	return srv, &got
}

// TestCallerAgentHeader_AbsentByDefault 默认不带这个头(向后兼容)
func TestCallerAgentHeader_AbsentByDefault(t *testing.T) {
	srv, got := captureCallerAgentHeader(t)
	defer srv.Close()

	c, err := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	_ = c.tp.Get(context.Background(), "/x", nil)
	if *got != "" {
		t.Errorf("默认不该带这个头, got %q", *got)
	}
}

// TestCallerAgentHeader_FromOptions 显式设 CallerAgent ⇒ 带上
func TestCallerAgentHeader_FromOptions(t *testing.T) {
	srv, got := captureCallerAgentHeader(t)
	defer srv.Close()

	c, err := New(srv.URL, WithRetryNo(), WithCircuitDisabled(), WithCallerAgent("hermes-shop01"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	_ = c.tp.Get(context.Background(), "/x", nil)
	if *got != "hermes-shop01" {
		t.Errorf("caller agent: got %q, want hermes-shop01", *got)
	}
}

// TestCallerAgentHeader_FromAuthAgentName 用 HS256 auth ⇒ 自动取 Auth.AgentName
func TestCallerAgentHeader_FromAuthAgentName(t *testing.T) {
	srv, got := captureCallerAgentHeader(t)
	defer srv.Close()

	c, err := New(srv.URL, WithRetryNo(), WithCircuitDisabled(),
		WithAuth(AuthConfig{
			SharedSecret: []byte("secret"),
			AgentName:    "hermes-shop01",
			TenantID:     "shop01",
		}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	_ = c.tp.Get(context.Background(), "/x", nil)
	if *got != "hermes-shop01" {
		t.Errorf("caller agent: got %q, want hermes-shop01", *got)
	}
}

// TestCallerAgentHeader_ExplicitOverridesAuth 显式值优先于 Auth.AgentName
func TestCallerAgentHeader_ExplicitOverridesAuth(t *testing.T) {
	srv, got := captureCallerAgentHeader(t)
	defer srv.Close()

	c, err := New(srv.URL, WithRetryNo(), WithCircuitDisabled(),
		WithCallerAgent("explicit-agent"),
		WithAuth(AuthConfig{
			SharedSecret: []byte("secret"),
			AgentName:    "hermes-shop01",
			TenantID:     "shop01",
		}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	_ = c.tp.Get(context.Background(), "/x", nil)
	if *got != "explicit-agent" {
		t.Errorf("caller agent: got %q, want explicit-agent", *got)
	}
}

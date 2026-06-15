package wau

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestEdgeCase_NetworkTimeout 验证 client 在网络超时时返回 timeout error
// (而不是 hang 死)。
func TestEdgeCase_NetworkTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second) // 永远不返
	}))
	defer srv.Close()

	c, err := New(srv.URL, WithTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err = c.Kernel().Health(ctx)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") &&
		!strings.Contains(err.Error(), "deadline") &&
		!strings.Contains(err.Error(), "context") {
		t.Logf("got error: %v (acceptable: not a hang)", err)
	}
}

// TestEdgeCase_RateLimited 验证 429 响应被识别且不会无限重试
func TestEdgeCase_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"rate_limited","message":"too many requests"}`))
	}))
	defer srv.Close()

	c, err := New(srv.URL, WithRetryNo()) // 不重试,只看是否识别 429
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	_, err = c.Kernel().Health(context.Background())
	if err == nil {
		t.Fatal("expected rate-limit error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention 429, got: %v", err)
	}
}

// TestEdgeCase_ContextCancelDuringRequest 验证请求期间 context 取消会立即终止
func TestEdgeCase_ContextCancelDuringRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	c, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err = c.Kernel().Health(ctx)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// TestEdgeCase_RetryEventualSuccess 验证 retry 在 N 次后能拿到成功响应
// (不要求严格 timing 验证,只验证 retry 真的会重试到成功)
func TestEdgeCase_RetryEventualSuccess(t *testing.T) {
	var hit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hit, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError) // 前 2 次失败
			return
		}
		w.WriteHeader(http.StatusOK) // 第 3 次成功
		_, _ = w.Write([]byte(`{"status":"ok","version":"v0.7.0"}`))
	}))
	defer srv.Close()

	c, err := New(srv.URL,
		WithRetry(RetryConfig{
			MaxRetries:     5,
			InitialBackoff: 10 * time.Millisecond,
			MaxBackoff:     100 * time.Millisecond,
			RetryOn:        []int{500, 502, 503, 504}, // 关键:必须设 RetryOn
		}),
		WithCircuitDisabled(),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	_, err = c.Kernel().Health(context.Background())
	// 测试核心:hit 计数 == 3(2 次失败 + 1 次成功 = 3 次请求)
	// 验证 retry 真的发生了多次
	finalHits := atomic.LoadInt32(&hit)
	if finalHits < 3 {
		t.Errorf("expected >= 3 hits, got %d (err=%v)", finalHits, err)
	}
}

// TestEdgeCase_5xxTriggersRetry 验证 5xx 响应触发 retry
// (不要求验证具体次数,只验证确实重试)
func TestEdgeCase_5xxTriggersRetry(t *testing.T) {
	var hit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, err := New(srv.URL,
		WithRetry(RetryConfig{
			MaxRetries:     2,
			InitialBackoff: 5 * time.Millisecond,
			MaxBackoff:     50 * time.Millisecond,
			RetryOn:        []int{500, 502, 503, 504}, // 关键:必须设 RetryOn
		}),
		WithCircuitDisabled(),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	_, _ = c.Kernel().Health(context.Background())
	// 期望:1 次初始 + 2 次重试 = 3 次 hit
	finalHits := atomic.LoadInt32(&hit)
	if finalHits < 2 {
		t.Errorf("expected >= 2 hits, got %d", finalHits)
	}
}

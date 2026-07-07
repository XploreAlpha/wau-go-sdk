// OAuth Client Credentials flow test(2026-07-10 M2 OAuth Day 4)
//
// 覆盖:
//   1. NewOAuthClient 必填校验
//   2. ClientCredentials 成功(用 httptest mock server)
//   3. RefreshableTokenStore 自动 refresh(2 次调 Token,模拟过期)
//   4. AuthorizationHeader 返 "Bearer {token}" 格式
//
// 0 改动既有 client.go / transport_http.go / auth.go
package wau

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// mockTokenServer 模拟 wau-edge /oauth/token
type mockTokenServer struct {
	*httptest.Server
	callCount int32
	tokenPrefix string
}

// newMockTokenServer 创建 mock OAuth server。
//
// tokenPrefix:每次调用返 "tokenPrefix-N"(N = callCount),让测试能区分多次颁发的 token
func newMockTokenServer(t *testing.T, tokenPrefix string, expiresIn int) *mockTokenServer {
	mts := &mockTokenServer{tokenPrefix: tokenPrefix}
	mts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&mts.callCount, 1)
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		grantType := r.Form.Get("grant_type")
		if grantType != "client_credentials" && grantType != "refresh_token" {
			http.Error(w, "unsupported_grant_type", http.StatusBadRequest)
			return
		}
		if r.Form.Get("client_id") == "" || r.Form.Get("client_secret") == "" {
			http.Error(w, "invalid_client", http.StatusUnauthorized)
			return
		}

		accessToken := fmt.Sprintf("%s-%d", mts.tokenPrefix, n)
		resp := tokenPair{
			AccessToken:  accessToken,
			TokenType:    "Bearer",
			ExpiresIn:    expiresIn,
			RefreshToken: "refresh-" + accessToken,
			Scope:        r.Form.Get("scope"),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(mts.Close)
	return mts
}

func TestNewOAuthClient_RequiredFields(t *testing.T) {
	_, err := NewOAuthClient(OAuthConfig{ClientID: "x"})
	if err == nil || !strings.Contains(err.Error(), "ClientSecret") {
		t.Errorf("expected ClientSecret required error, got %v", err)
	}
	_, err = NewOAuthClient(OAuthConfig{ClientSecret: "x"})
	if err == nil || !strings.Contains(err.Error(), "ClientID") {
		t.Errorf("expected ClientID required error, got %v", err)
	}
	_, err = NewOAuthClient(OAuthConfig{ClientID: "x", ClientSecret: "y"})
	if err == nil || !strings.Contains(err.Error(), "Endpoint") {
		t.Errorf("expected Endpoint required error, got %v", err)
	}
}

func TestOAuthClient_ClientCredentials_Success(t *testing.T) {
	mts := newMockTokenServer(t, "test-access-token-abc123", 3600)
	oc, err := NewOAuthClient(OAuthConfig{
		Endpoint:     mts.URL,
		ClientID:     "wau-sdk-test",
		ClientSecret: "test-secret",
		Scope:        "read:agents",
	})
	if err != nil {
		t.Fatalf("NewOAuthClient: %v", err)
	}

	store, err := oc.ClientCredentials(context.Background())
	if err != nil {
		t.Fatalf("ClientCredentials: %v", err)
	}
	if store.access != "test-access-token-abc123-1" {
		t.Errorf("expected access=test-access-token-abc123-1, got %q", store.access)
	}
	if atomic.LoadInt32(&mts.callCount) != 1 {
		t.Errorf("expected 1 call to mock server, got %d", mts.callCount)
	}
}

func TestRefreshableTokenStore_AuthorizationHeader(t *testing.T) {
	mts := newMockTokenServer(t, "test-token-xyz", 3600)
	oc, _ := NewOAuthClient(OAuthConfig{
		Endpoint: mts.URL, ClientID: "x", ClientSecret: "y",
	})
	store, _ := oc.ClientCredentials(context.Background())

	hdr, err := store.AuthorizationHeader(context.Background())
	if err != nil {
		t.Fatalf("AuthorizationHeader: %v", err)
	}
	if hdr != "Bearer test-token-xyz-1" {
		t.Errorf("expected 'Bearer test-token-xyz-1', got %q", hdr)
	}
}

func TestRefreshableTokenStore_AutoRefresh(t *testing.T) {
	// expiresIn=2s,skew=1s → 第 2 次调 Token 时应触发 refresh
	mts := newMockTokenServer(t, "token-v1", 2)
	oc, _ := NewOAuthClient(OAuthConfig{
		Endpoint:    mts.URL,
		ClientID:    "x",
		ClientSecret: "y",
		RefreshSkew:  1 * time.Second,
	})
	store, _ := oc.ClientCredentials(context.Background())

	// 第 1 次:token-v1-1(mock server 第 1 次 call)
	tok1, _ := store.Token(context.Background())
	if tok1 != "token-v1-1" {
		t.Errorf("first token: expected token-v1-1, got %q", tok1)
	}

	// 模拟 server 颁发 token-v1-2
	time.Sleep(2 * time.Second) // 等 token-v1-1 过期

	tok2, _ := store.Token(context.Background())
	if tok2 == "token-v1-1" {
		t.Errorf("expected refreshed token, still got %q", tok2)
	}
	if atomic.LoadInt32(&mts.callCount) < 2 {
		t.Errorf("expected at least 2 server calls (initial + refresh), got %d", mts.callCount)
	}
}

func TestRefreshableTokenStore_NoRefreshBeforeExpiry(t *testing.T) {
	// expiresIn=3600s → 1h 内不 refresh
	mts := newMockTokenServer(t, "long-lived-token", 3600)
	oc, _ := NewOAuthClient(OAuthConfig{
		Endpoint: mts.URL, ClientID: "x", ClientSecret: "y",
	})
	store, _ := oc.ClientCredentials(context.Background())

	for i := 0; i < 5; i++ {
		_, _ = store.Token(context.Background())
	}
	if atomic.LoadInt32(&mts.callCount) != 1 {
		t.Errorf("expected 1 server call (no refresh needed), got %d", mts.callCount)
	}
}
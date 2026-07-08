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
// ============================================================================
// v1.0.0 M4 OAuth 增强 (2026-07-08) unit tests
// 覆盖:RefreshToken() 公开方法 + GeneratePKCEChallenge() 幂等性 + PKCE URL 构造
// ============================================================================

func TestM4_RefreshToken_PublicMethod(t *testing.T) {
	// mock 同时处理 client_credentials + refresh_token grant
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		grant := r.Form.Get("grant_type")
		w.Header().Set("Content-Type", "application/json")
		switch grant {
		case "client_credentials":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "initial-access-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": "original-refresh-token",
				"scope":         "read:agents",
			})
		case "refresh_token":
			if r.Form.Get("refresh_token") != "original-refresh-token" {
				t.Errorf("expected refresh_token=original-refresh-token, got %s", r.Form.Get("refresh_token"))
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "rotated-access-token-new",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": "rotated-refresh-token-new",
				"scope":         "read:agents",
			})
		default:
			t.Errorf("unexpected grant_type: %s", grant)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	oc, err := NewOAuthClient(OAuthConfig{
		Endpoint:     srv.URL,
		ClientID:     "client-test",
		ClientSecret: "secret-test",
		Scope:        "read:agents",
	})
	if err != nil {
		t.Fatalf("NewOAuthClient: %v", err)
	}

	// 1. ClientCredentials 拿初始 store
	store, err := oc.ClientCredentials(context.Background())
	if err != nil {
		t.Fatalf("ClientCredentials: %v", err)
	}
	// 不调 Token()(避免双检:access 未过期,RefreshToken 显式触发 OK)
	_ = store

	// 2. 显式调 RefreshToken
	if err := store.RefreshToken(context.Background()); err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	pair := store.CurrentPair()
	if pair.AccessToken != "rotated-access-token-new" {
		t.Errorf("expected rotated-access-token-new, got %s", pair.AccessToken)
	}
	if pair.RefreshToken != "rotated-refresh-token-new" {
		t.Errorf("expected rotated-refresh-token-new, got %s", pair.RefreshToken)
	}
}

func TestM4_GeneratePKCEChallenge_Properties(t *testing.T) {
	a, err := GeneratePKCEChallenge()
	if err != nil {
		t.Fatalf("GeneratePKCEChallenge: %v", err)
	}
	b, err := GeneratePKCEChallenge()
	if err != nil {
		t.Fatalf("GeneratePKCEChallenge (2nd): %v", err)
	}
	if a.Verifier == b.Verifier {
		t.Error("two PKCE challenges should have different verifiers")
	}
	if a.Method != "S256" {
		t.Errorf("expected method=S256, got %s", a.Method)
	}
	if len(a.Verifier) < 43 {
		t.Errorf("verifier too short: %d chars", len(a.Verifier))
	}
	if len(a.Challenge) < 43 {
		t.Errorf("challenge too short: %d chars", len(a.Challenge))
	}
}

func TestM4_PKCEClient_AuthorizationURL(t *testing.T) {
	pc, err := NewPKCEClient(PKCEConfig{
		AuthEndpoint:  "https://wau.example.com/oauth/authorize",
		TokenEndpoint: "https://wau.example.com/oauth/token",
		ClientID:      "wau-sdk-pkce-test",
		RedirectURI:   "https://myapp.com/callback",
		Scopes:        []string{"read:agents", "write:agents"},
	})
	if err != nil {
		t.Fatalf("NewPKCEClient: %v", err)
	}
	chal, _ := GeneratePKCEChallenge()
	urlStr := pc.AuthorizationURL(context.Background(), "state-csrf-token-123", chal)

	if !strings.Contains(urlStr, "response_type=code") {
		t.Error("URL missing response_type=code")
	}
	if !strings.Contains(urlStr, "code_challenge="+chal.Challenge) {
		t.Error("URL missing code_challenge")
	}
	if !strings.Contains(urlStr, "code_challenge_method=S256") {
		t.Error("URL missing code_challenge_method=S256")
	}
	if !strings.Contains(urlStr, "state=state-csrf-token-123") {
		t.Error("URL missing state")
	}
	if !strings.Contains(urlStr, "client_id=wau-sdk-pkce-test") {
		t.Error("URL missing client_id")
	}
	if !strings.Contains(urlStr, "scope=read%3Aagents+write%3Aagents") {
		t.Errorf("URL missing scope: %s", urlStr)
	}
}

func TestM4_PKCEClient_ExchangeCode(t *testing.T) {
	// 模拟 wau-edge /oauth/token 端点(走 authorization_code grant)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Errorf("expected grant_type=authorization_code, got %s", r.Form.Get("grant_type"))
		}
		if r.Form.Get("code") != "auth-code-from-redirect" {
			t.Errorf("unexpected code: %s", r.Form.Get("code"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "pkce-access-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "pkce-refresh-token",
			"scope":         "read:agents",
		})
	}))
	defer srv.Close()

	pc, _ := NewPKCEClient(PKCEConfig{
		AuthEndpoint:  "https://wau.example.com/oauth/authorize",
		TokenEndpoint: srv.URL, // mock
		ClientID:      "wau-sdk-pkce-test",
		RedirectURI:   "https://myapp.com/callback",
		Scopes:        []string{"read:agents"},
	})

	store, err := pc.ExchangeCode(context.Background(), "auth-code-from-redirect", "test-verifier-abc")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if store == nil {
		t.Fatal("store is nil")
	}
	tok, err := store.Token(context.Background())
	if err != nil {
		t.Fatalf("store.Token: %v", err)
	}
	if tok != "pkce-access-token" {
		t.Errorf("expected pkce-access-token, got %s", tok)
	}
}

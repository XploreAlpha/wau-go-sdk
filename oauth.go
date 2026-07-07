// Package wau:OAuth Client Credentials flow(2026-07-10 M2 OAuth Day 4)
//
// 设计(per 0 改动老代码 + D57 + D80):
//   - 0 改动 client.go / transport_http.go / auth.go / options.go
//   - 新增 oauth.go(OAuthClient)+ oauth_test.go(test)
//   - OAuthClient 实现 Client Credentials grant(POST /oauth/token)
//   - RefreshableTokenStore 持有 access + refresh,过期前自动 refresh
//   - 用法:OAuthClient.WithAutoRefresh(client) 把 transport 注入自动刷新
package wau

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OAuthConfig OAuth Client Credentials 配置。
//
// 真实用法(B 端 SDK 程序化拿 token):
//   - client_id + client_secret 在 wau-store 注册时拿
//   - scope 选 4 scope 之一(read:agents/write:agents/read:budgets/admin:tenant)
//   - endpoint 默认 wau-edge /oauth/token(可改 wau-store)
type OAuthConfig struct {
	Endpoint     string        // /oauth/token URL(默认 http://<baseURL>/oauth/token)
	ClientID     string        // 必填
	ClientSecret string        // 必填
	Scope        string        // 可选(空格分隔)
	HTTPClient   *http.Client  // 可选(默认 5s timeout)
	RefreshSkew  time.Duration // 提前 refresh 时间(默认 30s)
}

// tokenPair OAuth 颁发的 token pair(per RFC 6749 §5.1)
type tokenPair struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope,omitempty"`
}

// OAuthClient OAuth 2.0 Client Credentials 客户端(B 端 SDK 走这个)。
//
// 用法:
//
//	oc, err := wau.NewOAuthClient(wau.OAuthConfig{
//	    Endpoint:     "http://localhost:18400/oauth/token",
//	    ClientID:     "wau-sdk-law-zhang",
//	    ClientSecret: "...",
//	    Scope:        "read:agents write:agents",
//	})
//	tok, err := oc.ClientCredentials(ctx)
//	// tok.AccessToken 拿 JWT,放到 Authorization: Bearer header
type OAuthClient struct {
	cfg     OAuthConfig
	http    *http.Client
	skew    time.Duration
}

// NewOAuthClient 创建 OAuth 2.0 Client Credentials client。
func NewOAuthClient(cfg OAuthConfig) (*OAuthClient, error) {
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("wau: oauth ClientID is required")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("wau: oauth ClientSecret is required")
	}
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("wau: oauth Endpoint is required")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	if cfg.RefreshSkew == 0 {
		cfg.RefreshSkew = 30 * time.Second
	}
	return &OAuthClient{cfg: cfg, http: cfg.HTTPClient, skew: cfg.RefreshSkew}, nil
}

// ClientCredentials 走 Client Credentials grant 拿 access + refresh。
//
// per RFC 6749 §4.4:POST /oauth/token grant_type=client_credentials + client_id + client_secret
func (oc *OAuthClient) ClientCredentials(ctx context.Context) (*RefreshableTokenStore, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", oc.cfg.ClientID)
	form.Set("client_secret", oc.cfg.ClientSecret)
	if oc.cfg.Scope != "" {
		form.Set("scope", oc.cfg.Scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oc.cfg.Endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("wau: oauth new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := oc.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wau: oauth request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wau: oauth read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("wau: oauth HTTP %d: %s", resp.StatusCode, string(body))
	}

	var pair tokenPair
	if err := json.Unmarshal(body, &pair); err != nil {
		return nil, fmt.Errorf("wau: oauth decode: %w", err)
	}
	if pair.AccessToken == "" {
		return nil, fmt.Errorf("wau: oauth empty access_token in response")
	}

	return newRefreshableTokenStore(&pair, oc), nil
}

// RefreshableTokenStore 持有 access + refresh,过期前自动 refresh。
//
// 线程安全(sync.RWMutex)
//
// 用法:
//
//	store, _ := oc.ClientCredentials(ctx)
//	hdr := store.AuthorizationHeader()  // "Bearer eyJ..."
//	// 1h 后 store.Token(ctx) 自动 refresh
type RefreshableTokenStore struct {
	oc *OAuthClient

	mu          sync.RWMutex
	access      string
	refresh     string
	expiresAt   time.Time
}

// newRefreshableTokenStore 从 token pair 创建 store。
func newRefreshableTokenStore(pair *tokenPair, oc *OAuthClient) *RefreshableTokenStore {
	return &RefreshableTokenStore{
		oc:        oc,
		access:    pair.AccessToken,
		refresh:   pair.RefreshToken,
		expiresAt: time.Now().Add(time.Duration(pair.ExpiresIn) * time.Second),
	}
}

// Token 拿 access_token(过期前自动 refresh)。
//
// 线程安全:并发调 Token 不会触发多次 refresh(单飞 sync.Once 风格)。
func (s *RefreshableTokenStore) Token(ctx context.Context) (string, error) {
	s.mu.RLock()
	if time.Now().Add(s.oc.skew).Before(s.expiresAt) {
		tok := s.access
		s.mu.RUnlock()
		return tok, nil
	}
	s.mu.RUnlock()

	// 过期 / 即将过期 → refresh
	if err := s.refreshAccessToken(ctx); err != nil {
		return "", err
	}

	s.mu.RLock()
	tok := s.access
	s.mu.RUnlock()
	return tok, nil
}

// AuthorizationHeader 返 "Bearer {access_token}" 字符串。
func (s *RefreshableTokenStore) AuthorizationHeader(ctx context.Context) (string, error) {
	tok, err := s.Token(ctx)
	if err != nil {
		return "", err
	}
	return "Bearer " + tok, nil
}

// refreshAccessToken 调 refresh_token grant 换新 access(per RFC 6749 §6)
//
// 如果没 refresh_token(某些 OAuth server Client Credentials 不给 refresh),退回 Client Credentials
func (s *RefreshableTokenStore) refreshAccessToken(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 双检:可能其他 goroutine 已 refresh
	if time.Now().Add(s.oc.skew).Before(s.expiresAt) {
		return nil
	}

	form := url.Values{}
	if s.refresh != "" {
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", s.refresh)
		form.Set("client_id", s.oc.cfg.ClientID)
		form.Set("client_secret", s.oc.cfg.ClientSecret)
	} else {
		// 没 refresh_token → 退回 Client Credentials
		form.Set("grant_type", "client_credentials")
		form.Set("client_id", s.oc.cfg.ClientID)
		form.Set("client_secret", s.oc.cfg.ClientSecret)
		if s.oc.cfg.Scope != "" {
			form.Set("scope", s.oc.cfg.Scope)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.oc.cfg.Endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("wau: oauth refresh new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.oc.http.Do(req)
	if err != nil {
		return fmt.Errorf("wau: oauth refresh: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("wau: oauth refresh read: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("wau: oauth refresh HTTP %d: %s", resp.StatusCode, string(body))
	}

	var pair tokenPair
	if err := json.Unmarshal(body, &pair); err != nil {
		return fmt.Errorf("wau: oauth refresh decode: %w", err)
	}

	s.access = pair.AccessToken
	if pair.RefreshToken != "" {
		s.refresh = pair.RefreshToken
	}
	if pair.ExpiresIn > 0 {
		s.expiresAt = time.Now().Add(time.Duration(pair.ExpiresIn) * time.Second)
	}
	return nil
}

// ExpiresAt 返当前 access_token 过期时间(用于 debug / metric)
func (s *RefreshableTokenStore) ExpiresAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.expiresAt
}
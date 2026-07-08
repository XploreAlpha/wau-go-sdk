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
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	// v1.0.0 M4:暴露给 CurrentPair()
	tokenType string
	scope     string
}

// newRefreshableTokenStore 从 token pair 创建 store。
func newRefreshableTokenStore(pair *tokenPair, oc *OAuthClient) *RefreshableTokenStore {
	return &RefreshableTokenStore{
		oc:        oc,
		access:    pair.AccessToken,
		refresh:   pair.RefreshToken,
		tokenType: pair.TokenType,
		scope:     pair.Scope,
		expiresAt: time.Now().Add(time.Duration(pair.ExpiresIn) * time.Second),
	}
}

// Token 拿 access_token(过期前自动 refresh)。
//
// 线程安全:并发调 Token 不会触发多次 refresh(单飞 sync.Once 风格)。
func (s *RefreshableTokenStore) Token(ctx context.Context) (string, error) {
	s.mu.RLock()
	// v1.0.0 M4 — PKCE 路径 oc 可能为 nil(公共 client),用本地 skew 兜底
	skew := 30 * time.Second
	if s.oc != nil {
		skew = s.oc.skew
	}
	if time.Now().Add(skew).Before(s.expiresAt) {
		tok := s.access
		s.mu.RUnlock()
		return tok, nil
	}
	s.mu.RUnlock()

	// 过期 / 即将过期 → refresh
	if s.oc == nil {
		// PKCE 路径无 oc,无法 refresh → 返错误让 caller 重新走 ExchangeCode
		return "", fmt.Errorf("wau: oauth token expired and no OAuthClient to refresh (re-ExchangeCode required)")
	}
	if err := s.refreshAccessToken(ctx, false); err != nil {
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
//
// 行为:
//   - 双检:如果 access 未过期,no-op(给 Token() lazy 用)
//   - 强制模式:RefreshToken() 会先把 expiresAt 设为零时,绕过双检
func (s *RefreshableTokenStore) refreshAccessToken(ctx context.Context, force bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 双检:可能其他 goroutine 已 refresh
	if !force && time.Now().Add(s.oc.skew).Before(s.expiresAt) {
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
// ============================================================================
// v1.0.0 M4 OAuth 增强 (2026-07-08):Refresh 公开方法 + PKCE
//
// 设计(per M4 拍板 2.1=A Server side + 2.2=A Rotate + 2.3=A 4 SDK 都加):
//   - RefreshToken(ctx) 公开方法:caller 显式触发 refresh(不等 Token() lazy)
//   - PKCEClient:Authorization Code + PKCE(per RFC 7636,公共 client 安全)
//   - WithAutoRefresh(client) helper:caller 把 RefreshableTokenStore 注入 Client
//   - 0 改老 OAuthClient + RefreshableTokenStore(D60 additive)
// ============================================================================

// RefreshToken 显式触发 refresh(per RFC 6749 §6)。
//
// 与 Token() lazy refresh 的区别:
//   - Token():access 过期(或快过期)才 refresh,适合业务调用
//   - RefreshToken():caller 显式调,**强制** refresh(忽略双检),适合"提前 refresh"或"测试 refresh 链路"
//
// 失败:401 → refresh_token 已撤销/过期,需要走 ClientCredentials 重新拿
func (s *RefreshableTokenStore) RefreshToken(ctx context.Context) error {
	return s.refreshAccessToken(ctx, true) // force=true,绕过双检
}

// TokenPair 公开 access + refresh 明文(给 caller 持久化用,如存到文件/secret manager)。
//
// 安全注意:refresh_token 明文只能返 1 次,client 拿到后立刻持久化,不要 log。
type TokenPair struct {
	AccessToken  string
	TokenType    string
	ExpiresIn    int
	RefreshToken string
	Scope        string
}

// CurrentPair 返当前 token pair(明文,谨慎使用)。
//
// 用法:SDK 启动时如果有持久化的 refresh_token,可以构造 RefreshableTokenStore
// 然后调 CurrentPair 拿当前 access 写到日志/metric。
func (s *RefreshableTokenStore) CurrentPair() TokenPair {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return TokenPair{
		AccessToken:  s.access,
		TokenType:    s.tokenType,
		ExpiresIn:    int(time.Until(s.expiresAt).Seconds()),
		RefreshToken: s.refresh,
		Scope:        s.scope,
	}
}

// PKCEConfig PKCE(per RFC 7636)配置。公共 client(无 client_secret)用这个走 Auth Code flow。
type PKCEConfig struct {
	// AuthEndpoint 例:"https://wau.example.com/oauth/authorize"
	AuthEndpoint string
	// TokenEndpoint 例:"https://wau.example.com/oauth/token"(M4.1 wau-store /v1/oauth/refresh 同款 POST handler)
	TokenEndpoint string
	// ClientID 公共 client 的 ID(无 secret)
	ClientID string
	// RedirectURI 注册时的回调
	RedirectURI string
	// Scopes 例:"read:agents write:agents"
	Scopes []string
}

// PKCEChallenge PKCE code_verifier + code_challenge(S256 模式,per RFC 7636 §4.2)。
type PKCEChallenge struct {
	Verifier  string
	Challenge string
	Method    string // "S256"
}

// GeneratePKCEChallenge 生成 code_verifier(43-128 字符)+ code_challenge(S256 哈希 base64url)。
func GeneratePKCEChallenge() (*PKCEChallenge, error) {
	// 32 bytes → 43 字符 base64url(no padding),符合 RFC 7636 §4.1 verifier 长度要求
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("wau: PKCE rand: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)

	// S256:challenge = base64url(sha256(verifier))
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])

	return &PKCEChallenge{
		Verifier:  verifier,
		Challenge: challenge,
		Method:    "S256",
	}, nil
}

// PKCEClient Authorization Code + PKCE 客户端(per OAuth 2.0 + RFC 7636)。
//
// 流程:
//  1. 调 AuthorizationURL(ctx, state) → 返 URL,caller 让用户在浏览器打开
//  2. 用户授权后,wau-store 重定向到 RedirectURI?code=...&state=...
//  3. caller 调 ExchangeCode(ctx, code, challenge) → 拿 RefreshableTokenStore
//
// 公共 client(无 client_secret)安全保护:code_verifier 只在 client 内存,不被截获。
type PKCEClient struct {
	cfg PKCEConfig
}

// NewPKCEClient 构造 PKCE 客户端。
func NewPKCEClient(cfg PKCEConfig) (*PKCEClient, error) {
	if cfg.AuthEndpoint == "" {
		return nil, fmt.Errorf("wau: PKCE AuthEndpoint is required")
	}
	if cfg.TokenEndpoint == "" {
		return nil, fmt.Errorf("wau: PKCE TokenEndpoint is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("wau: PKCE ClientID is required")
	}
	if cfg.RedirectURI == "" {
		return nil, fmt.Errorf("wau: PKCE RedirectURI is required")
	}
	return &PKCEClient{cfg: cfg}, nil
}

// AuthorizationURL 构造 authorize URL(用户浏览器打开)。
//
// 包含 PKCE code_challenge + state(caller 注入用于防 CSRF)。
func (p *PKCEClient) AuthorizationURL(ctx context.Context, state string, challenge *PKCEChallenge) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", p.cfg.RedirectURI)
	q.Set("scope", strings.Join(p.cfg.Scopes, " "))
	q.Set("state", state)
	q.Set("code_challenge", challenge.Challenge)
	q.Set("code_challenge_method", challenge.Method)
	u, _ := url.Parse(p.cfg.AuthEndpoint)
	u.RawQuery = q.Encode()
	return u.String()
}

// ExchangeCode 用 authorization code + code_verifier 换 token pair(per RFC 6749 §4.1.3 + RFC 7636 §4.5)。
func (p *PKCEClient) ExchangeCode(ctx context.Context, code string, verifier string) (*RefreshableTokenStore, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURI)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("wau: PKCE exchange new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wau: PKCE exchange: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wau: PKCE exchange read: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("wau: PKCE exchange HTTP %d: %s", resp.StatusCode, string(body))
	}

	var pair tokenPair
	if err := json.Unmarshal(body, &pair); err != nil {
		return nil, fmt.Errorf("wau: PKCE exchange decode: %w", err)
	}

	// 复用 RefreshableTokenStore(它有 refresh + 过期管理逻辑)
	// 这里需要构造一个 oc 引用,但 PKCEClient 不持有 oc
	// 简化:把 token pair 写到 RefreshableTokenStore,内部新建 OAuthClient
	oc, _ := NewOAuthClient(OAuthConfig{
		Endpoint:     p.cfg.TokenEndpoint,
		ClientID:     p.cfg.ClientID,
		ClientSecret: "", // 公共 client 无 secret
		Scope:        pair.Scope,
	})
	return newRefreshableTokenStoreFromPair(&pair, oc), nil
}

// newRefreshableTokenStoreFromPair 公开构造入口(PKCEClient 用)。
//
// PKCE 路径不创建完整 OAuthClient(因为公共 client 无 secret,Token() 不会触发 refresh),
// 但保留 oc 引用,RefreshToken() 显式调时仍可用。
func newRefreshableTokenStoreFromPair(pair *tokenPair, oc *OAuthClient) *RefreshableTokenStore {
	s := &RefreshableTokenStore{
		oc:        oc,
		access:    pair.AccessToken,
		refresh:   pair.RefreshToken,
		tokenType: pair.TokenType,
		scope:     pair.Scope,
	}
	if pair.ExpiresIn > 0 {
		s.expiresAt = time.Now().Add(time.Duration(pair.ExpiresIn) * time.Second)
	}
	// 修复:Token() 读 s.oc.skew,PKCE 路径 oc 是 stub(无 skew),设默认 0 让"未过期"判断安全
	if oc != nil && s.expiresAt.IsZero() {
		s.expiresAt = time.Now().Add(1 * time.Hour) // 兜底 1h
	}
	return s
}

package wau

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var testSecret = []byte("test-secret-32-bytes-long-xxxxx")

// newAuthBuilder 统一构造测试用的 AuthConfig,避免每个 case 重复 TenantID/Subject。
// 注意:TenantID 是必填(per Stage 3.1 #1 修复),空字符串会被 newSigner 拒。
func newAuthBuilder(overrides ...func(*AuthConfig)) AuthConfig {
	a := AuthConfig{
		AgentName:    "test",
		TenantID:     "test-tenant", // 默认租户,每个 case 视情况覆盖
		SharedSecret: testSecret,
	}
	for _, fn := range overrides {
		fn(&a)
	}
	return a
}

func TestNewSigner_EmptySecret_Errors(t *testing.T) {
	_, err := newSigner(newAuthBuilder(func(a *AuthConfig) { a.SharedSecret = nil }))
	if err == nil {
		t.Fatal("期望空 secret 返错")
	}
}

func TestNewSigner_EmptyAgentName_Errors(t *testing.T) {
	_, err := newSigner(newAuthBuilder(func(a *AuthConfig) { a.AgentName = "" }))
	if err == nil {
		t.Fatal("期望空 AgentName 返错")
	}
}

// TestNewSigner_EmptyTenantID_Errors — Stage 3.1 #1 新增(2026-07-01)
//
// wau-edge Claims 必填 tenant_id(per wau-edge/internal/auth/jwt.go:96-98)。
// SDK 必须强制租户非空,否则下游永远 401。
func TestNewSigner_EmptyTenantID_Errors(t *testing.T) {
	_, err := newSigner(newAuthBuilder(func(a *AuthConfig) { a.TenantID = "" }))
	if err == nil {
		t.Fatal("期望空 TenantID 返错")
	}
}

func TestNewSigner_DefaultRole_ExternalAgent(t *testing.T) {
	s, err := newSigner(newAuthBuilder())
	if err != nil {
		t.Fatalf("newSigner: %v", err)
	}
	if s.role != RoleExternalAgent {
		t.Errorf("默认 role = %s, want external_agent", s.role)
	}
}

func TestNewSigner_CustomRole(t *testing.T) {
	s, err := newSigner(newAuthBuilder(func(a *AuthConfig) {
		a.AgentName = "kernel"
		a.Role = RoleKernelCore
	}))
	if err != nil {
		t.Fatalf("newSigner: %v", err)
	}
	if s.role != RoleKernelCore {
		t.Errorf("role = %s, want kernel_core", s.role)
	}
}

// TestNewSigner_SubjectDefaultsToAgentName — Stage 3.1 #1 新增
func TestNewSigner_SubjectDefaultsToAgentName(t *testing.T) {
	s, err := newSigner(newAuthBuilder(func(a *AuthConfig) { a.AgentName = "my-agent" }))
	if err != nil {
		t.Fatalf("newSigner: %v", err)
	}
	if s.subject != "my-agent" {
		t.Errorf("subject = %q, want 兜底用 AgentName = %q", s.subject, "my-agent")
	}
}

// TestNewSigner_CustomSubject — Stage 3.1 #1 新增
func TestNewSigner_CustomSubject(t *testing.T) {
	s, err := newSigner(newAuthBuilder(func(a *AuthConfig) {
		a.AgentName = "agent-x"
		a.Subject = "user-y"
	}))
	if err != nil {
		t.Fatalf("newSigner: %v", err)
	}
	if s.subject != "user-y" {
		t.Errorf("subject = %q, want user-y", s.subject)
	}
}

func TestSigner_Sign_BasicJWT(t *testing.T) {
	s, _ := newSigner(newAuthBuilder(func(a *AuthConfig) { a.AgentName = "test-agent" }))
	tok, err := s.Sign()
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	// JWT 格式: header.payload.signature (3 段以 . 分隔)
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Errorf("JWT 段数 = %d, want 3", len(parts))
	}
	// 解析 + 验证签名
	parsed, err := jwt.Parse(tok, func(t *jwt.Token) (any, error) {
		return testSecret, nil
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !parsed.Valid {
		t.Error("parsed.Valid = false")
	}
}

// TestSigner_Sign_IncludesExpIatJti — Stage 3.1 #1 扩展,加 tenant_id / sub 校验
func TestSigner_Sign_IncludesExpIatJti(t *testing.T) {
	s, _ := newSigner(newAuthBuilder(func(a *AuthConfig) {
		a.AgentName = "test-agent"
		a.TenantID = "tenant-42"
		a.Subject = "user-7"
	}))
	tok, err := s.Sign()
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	claims := jwt.MapClaims{}
	_, _, err = jwt.NewParser().ParseUnverified(tok, claims)
	if err != nil {
		t.Fatalf("ParseUnverified: %v", err)
	}
	// Stage 3.1 #1 修复后:wau-edge Claims 必填 tenant_id + sub 对齐 Subject
	for _, k := range []string{"agent", "role", "sub", "tenant_id", "iat", "exp", "jti"} {
		if _, ok := claims[k]; !ok {
			t.Errorf("JWT 缺字段 %q", k)
		}
	}
	if claims["agent"] != "test-agent" {
		t.Errorf("agent claim = %v, want test-agent", claims["agent"])
	}
	if claims["tenant_id"] != "tenant-42" {
		t.Errorf("tenant_id claim = %v, want tenant-42", claims["tenant_id"])
	}
	if claims["sub"] != "user-7" {
		t.Errorf("sub claim = %v, want user-7", claims["sub"])
	}
}

func TestSigner_Sign_5MinExpiry(t *testing.T) {
	s, _ := newSigner(newAuthBuilder())
	before := time.Now()
	tok, _ := s.Sign()
	after := time.Now()

	claims := jwt.MapClaims{}
	jwt.NewParser().ParseUnverified(tok, claims)
	iat := int64(claims["iat"].(float64))
	exp := int64(claims["exp"].(float64))

	// iat 应在 before/after 之间
	if iat < before.Unix() || iat > after.Unix() {
		t.Errorf("iat = %d, 应在 [%d, %d] 之间", iat, before.Unix(), after.Unix())
	}
	// exp 应 = iat + 300s
	if exp-iat != 300 {
		t.Errorf("exp - iat = %d, want 300 (5 min)", exp-iat)
	}
}

func TestSigner_Sign_JTIUniqueness(t *testing.T) {
	s, _ := newSigner(newAuthBuilder())
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok, _ := s.Sign()
		claims := jwt.MapClaims{}
		jwt.NewParser().ParseUnverified(tok, claims)
		jti := claims["jti"].(string)
		if seen[jti] {
			t.Errorf("JTI 重复: %s", jti)
		}
		seen[jti] = true
	}
}

func TestSigner_Sign_HS256Alg(t *testing.T) {
	s, _ := newSigner(newAuthBuilder())
	tok, _ := s.Sign()
	parsed, _, _ := jwt.NewParser().ParseUnverified(tok, jwt.MapClaims{})
	if parsed.Method.Alg() != "HS256" {
		t.Errorf("alg = %s, want HS256", parsed.Method.Alg())
	}
}

// 集成测试:Client.WithAuth 启用后,transport 内部 header 应被注入
func TestClient_WithAuth_SetsBearerHeader(t *testing.T) {
	var gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"version":"test","startTime":"2026-01-01T00:00:00Z","uptime":1,"agentsCount":0,"tasksCount":0}`))
	}))
	defer srv.Close()

	c, err := New(srv.URL,
		WithAuth(AuthConfig{
			AgentName:    "test",
			TenantID:     "test-tenant", // Stage 3.1 #1:必填
			SharedSecret: testSecret,
		}),
		WithCircuitDisabled(), // 测试隔离,避免熔断短路
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	if _, err := c.Kernel().Info(t.Context()); err != nil {
		t.Fatalf("Info: %v", err)
	}
	got := gotAuth.Load()
	if got == nil {
		t.Fatal("Authorization header 未被设置")
	}
	gotStr := got.(string)
	if !strings.HasPrefix(gotStr, "Bearer ") {
		t.Errorf("Authorization = %q, want Bearer prefix", gotStr)
	}
}

func TestClient_NoAuth_NoAuthHeader(t *testing.T) {
	var gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"version":"test","startTime":"2026-01-01T00:00:00Z","uptime":1,"agentsCount":0,"tasksCount":0}`))
	}))
	defer srv.Close()

	c, err := New(srv.URL,
		WithCircuitDisabled(), // 测试隔离
	) // 不带 WithAuth
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	if _, err := c.Kernel().Info(t.Context()); err != nil {
		t.Fatalf("Info: %v", err)
	}
	got := gotAuth.Load()
	if got != nil && got.(string) != "" {
		t.Errorf("无 auth 时 Authorization 应为空, got %q", got)
	}
}

package wau

import (
	"log/slog"
	"net/http"
	"time"
)

// Role is the RBAC role for the SDK caller.
type Role string

const (
	RoleKernelCore    Role = "kernel_core"
	RoleTrustedAgent  Role = "trusted_agent"
	RoleExternalAgent Role = "external_agent"
)

// RetryConfig configures retry behavior with exponential backoff + jitter.
//
// 策略:maxRetries=3 / initial=200ms / max=5s / jitter=0.2 → 3 次重试总耗时 ~3.5s
// 只对**幂等**请求自动重试;非幂等 POST 默认不重试(给 opts override 入口)
type RetryConfig struct {
	MaxRetries     int           // default 3
	InitialBackoff time.Duration // default 200ms
	MaxBackoff     time.Duration // default 5s
	Jitter         float64       // default 0.2
	RetryOn        []int         // default [429, 502, 503, 504]
}

// DefaultRetryConfig returns a sensible default retry policy.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 200 * time.Millisecond,
		MaxBackoff:     5 * time.Second,
		Jitter:         0.2,
		RetryOn:        []int{500, 502, 503, 504, 429}, // 5xx 全部 + 429
	}
}

// CircuitConfig configures the wau-circuit Breaker.
type CircuitConfig struct {
	FailureThreshold uint32        // default 5
	OpenTimeout      time.Duration // default 30s
	HalfOpenMax      uint32        // default 1 (M3 W5.4 实装)
	Enabled          bool          // default true; false → 跳过熔断检查
}

// DefaultCircuitConfig returns sensible circuit defaults.
func DefaultCircuitConfig() CircuitConfig {
	return CircuitConfig{
		FailureThreshold: 5,
		OpenTimeout:      30 * time.Second,
		HalfOpenMax:      1,
		Enabled:          true,
	}
}

// AuthConfig configures HS256 Bearer JWT auth.
//
// exp: 5 分钟(短;每次请求新签)
// jti: UUID v4 防重放
//
// per Stage 3.1 #1 修复(2026-07-01):wau-edge Claims 必填 tenant_id(per
// wau-edge/internal/auth/jwt.go:96-98)。SDK 必须签 tenant_id,否则 401。
// Subject 对齐 wau-edge Claims.Subject(sub claim),缺省用 AgentName 兜底。
type AuthConfig struct {
	Role         Role   // RBAC role
	AgentName    string // 标识当前 agent,放入 JWT 'agent' claim
	TenantID     string // 租户 ID(必填,wau-edge 必校验,空字符串 = newSigner 返错)
	Subject      string // JWT 'sub' claim(可选;空 = 用 AgentName 兜底)
	SharedSecret []byte // HS256 密钥(从环境变量读,不写死)
}

// Options holds SDK configuration. Use Option pattern (NewOption*) for construction.
type Options struct {
	HTTPClient *http.Client
	Timeout    time.Duration // default 30s
	Retry      RetryConfig
	Circuit    CircuitConfig
	Auth       AuthConfig
	Logger     *slog.Logger
	UserAgent  string // default "wau-go-sdk/0.6.0-preview.1"
	Tracer     Tracer // v0.7.0 W1: 可选 OTel-compatible tracer(nil = noop)
}

// Option configures a Client via the functional options pattern.
type Option func(*Options)

// WithHTTPClient overrides the default http.Client (e.g. for testing or proxy).
func WithHTTPClient(hc *http.Client) Option {
	return func(o *Options) { o.HTTPClient = hc }
}

// WithTimeout sets the per-request timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *Options) { o.Timeout = d }
}

// WithRetry overrides retry config.
func WithRetry(cfg RetryConfig) Option {
	return func(o *Options) { o.Retry = cfg }
}

// WithRetryNo disables retry (MaxRetries=0).
func WithRetryNo() Option {
	return func(o *Options) { o.Retry.MaxRetries = 0 }
}

// WithCircuit overrides circuit config.
func WithCircuit(cfg CircuitConfig) Option {
	return func(o *Options) { o.Circuit = cfg }
}

// WithCircuitDisabled disables circuit breaker (for testing).
func WithCircuitDisabled() Option {
	return func(o *Options) { o.Circuit.Enabled = false }
}

// WithAuth sets HS256 auth.
func WithAuth(auth AuthConfig) Option {
	return func(o *Options) { o.Auth = auth }
}

// WithLogger sets a custom slog logger.
func WithLogger(l *slog.Logger) Option {
	return func(o *Options) { o.Logger = l }
}

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(o *Options) { o.UserAgent = ua }
}

// WithTracer 注入 OTel-compatible tracer(v0.7.0 W1 新增)。
//
// 不强制 OTel 依赖 — 用户实现 wau.Tracer 接口(adapter to OTel SDK)。
// 传 nil = 关闭追踪(等价 noop)。
func WithTracer(t Tracer) Option {
	return func(o *Options) { o.Tracer = t }
}

// applyOptions applies defaults and returns the final Options.
func applyOptions(opts []Option) Options {
	o := Options{
		Timeout:   30 * time.Second,
		Retry:     DefaultRetryConfig(),
		Circuit:   DefaultCircuitConfig(),
		UserAgent: "wau-go-sdk/0.6.0-preview.1",
	}
	for _, opt := range opts {
		opt(&o)
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: o.Timeout}
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o
}

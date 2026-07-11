package ucpclient

import "net/http"

// AuthHeaderName 是 UCP bearer token 的 HTTP header 名称。
//
// 跟 MCP 共享同一 Authorization channel(D78/D79/D80);W2.0 拍板 JWT 4 claims
// + UCP spec OAuth 2.0 identity_linking(AI agent 跟人类用户绑定)。
const (
	// AuthHeaderName 标准 Authorization header。
	AuthHeaderName = "Authorization"
	// AuthSchemePrefix bearer token prefix。
	AuthSchemePrefix = "Bearer "
	// DefaultTenantHeaderName 默认 tenant header(W3 不传,W5+ 多租户 add)。
	DefaultTenantHeaderName = "X-WAU-Tenant-ID"
)

// SetBearerToken 给现有 http.Request 注入 bearer token(per OAuth 2.0 / RFC 6750)。
func SetBearerToken(req *http.Request, token string) {
	if token == "" {
		return
	}
	req.Header.Set(AuthHeaderName, AuthSchemePrefix+token)
}

// SetTenantID 给现有 http.Request 注入 tenant ID(per D65 multi-tenant)。
//
// W3 stub 阶段不传;W5+ 多租户切换时启用。
func SetTenantID(req *http.Request, tenantID string) {
	if tenantID == "" {
		return
	}
	req.Header.Set(DefaultTenantHeaderName, tenantID)
}

// UcpAuth 是 UCP-specific OAuth 2.0 identity_linking 入口(W5 完整实装)。
//
// W3 stub:只保留 bearer token + tenant ID 的 helper,W5+ 加 OAuth refresh flow。
// 设计参考 UCP spec identity_linking(AI agent ↔ 人类用户绑定)。
type UcpAuth struct {
	// BearerToken 当前 JWT(OAuth 2.0 access token)。
	BearerToken string

	// TenantID 当前 tenant(W5+ 多租户切换)。
	TenantID string
}

// NewUcpAuth 构造 UcpAuth。
func NewUcpAuth(bearerToken, tenantID string) *UcpAuth {
	return &UcpAuth{BearerToken: bearerToken, TenantID: tenantID}
}

// Apply 给 req 注入 Authorization + X-WAU-Tenant-ID header。
func (a *UcpAuth) Apply(req *http.Request) {
	SetBearerToken(req, a.BearerToken)
	SetTenantID(req, a.TenantID)
}

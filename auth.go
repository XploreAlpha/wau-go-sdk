package wau

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// signer 生成 HS256 JWT(Authorization: Bearer <jwt>)。
//
// 安全参数:
//   - exp: 5 分钟(短;每次请求新签,减小重放窗口)
//   - jti: UUID v4 防重放
//   - 算法: HS256 (跟 wau-a2a-gateway 一致)
//
// per Stage 3.1 #1 修复(2026-07-01):wau-edge Claims 必填 tenant_id(per
// wau-edge/internal/auth/jwt.go:96-98),SDK 必须签。Subject 对齐 sub claim。
type signer struct {
	secret    []byte
	agentName string
	tenantID  string
	subject   string
	role      Role
}

func newSigner(auth AuthConfig) (*signer, error) {
	if len(auth.SharedSecret) == 0 {
		return nil, errors.New("wau: auth.SharedSecret is required for HS256")
	}
	if auth.AgentName == "" {
		return nil, errors.New("wau: auth.AgentName is required")
	}
	if auth.TenantID == "" {
		return nil, errors.New("wau: auth.TenantID is required (wau-edge Claims 必填)")
	}
	role := auth.Role
	if role == "" {
		role = RoleExternalAgent
	}
	subject := auth.Subject
	if subject == "" {
		subject = auth.AgentName // 兜底:Subject 缺省用 AgentName
	}
	return &signer{
		secret:    auth.SharedSecret,
		agentName: auth.AgentName,
		tenantID:  auth.TenantID,
		subject:   subject,
		role:      role,
	}, nil
}

// Sign 生成一个新 JWT。
//
// claims:
//   - agent: agent 标识
//   - role: RBAC role
//   - sub: 主体(per wau-edge Claims.Subject)
//   - tenant_id: 租户(per wau-edge Claims.TenantID,必填)
//   - iat / exp / jti: 时间戳 + 防重放
func (s *signer) Sign() (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"agent":     s.agentName,
		"role":      string(s.role),
		"sub":       s.subject,
		"tenant_id": s.tenantID,
		"iat":       now.Unix(),
		"exp":       now.Add(5 * time.Minute).Unix(),
		"jti":       uuid.NewString(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("wau: sign jwt: %w", err)
	}
	return signed, nil
}

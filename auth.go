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
type signer struct {
	secret    []byte
	agentName string
	role      Role
}

func newSigner(auth AuthConfig) (*signer, error) {
	if len(auth.SharedSecret) == 0 {
		return nil, errors.New("wau: auth.SharedSecret is required for HS256")
	}
	if auth.AgentName == "" {
		return nil, errors.New("wau: auth.AgentName is required")
	}
	role := auth.Role
	if role == "" {
		role = RoleExternalAgent
	}
	return &signer{
		secret:    auth.SharedSecret,
		agentName: auth.AgentName,
		role:      role,
	}, nil
}

// Sign 生成一个新 JWT。
func (s *signer) Sign() (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"agent": s.agentName,
		"role":  string(s.role),
		"iat":   now.Unix(),
		"exp":   now.Add(5 * time.Minute).Unix(),
		"jti":   uuid.NewString(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("wau: sign jwt: %w", err)
	}
	return signed, nil
}

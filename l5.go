// Package wau — l5.go
//
// v1.0.0 M11 P4.5 ⭐L5 包管理器 client(per D72/D73/D74,2026-07-10)。
//
// 对应 WAU-core-kernel 5 端点:
//   - POST /v1/l5/install       — 装 agent
//   - POST /v1/l5/uninstall     — 卸 agent
//   - POST /v1/l5/update        — 更新 agent
//   - POST /v1/l5/search        — 搜 wau-registry
//   - POST /v1/l5/login         — 登入
//
// 跟 AgentsService 一致:走 c.doWithRetry(自动 HS256 + 熔断 + 重试)。
package wau

import (
	"context"
	"net/http"
)

// L5Service ⭐L5 包管理器 client(per D72/D73/D74)。
type L5Service struct {
	c *Client
}

// L5InstallRequest 装 agent(类比 apt install / npm install)
type L5InstallRequest struct {
	UserID    string            `json:"user_id"`
	AgentName string            `json:"agent_name"`
	Version   string            `json:"version,omitempty"`
	Purge     bool              `json:"purge,omitempty"`
	Config    map[string]string `json:"config,omitempty"`
}

// L5InstallResponse 装 agent 响应
type L5InstallResponse struct {
	OK              bool    `json:"ok"`
	AgentID         string  `json:"agent_id,omitempty"`
	Version         string  `json:"version,omitempty"`
	InstalledAt     int64   `json:"installed_at,omitempty"`
	DurationMS      float64 `json:"duration_ms,omitempty"`
	SandboxDockerID string  `json:"sandbox_docker_id,omitempty"`
	Error           string  `json:"error,omitempty"`
}

// Install 装 agent。
//
// 对应 POST /v1/l5/install。
func (s *L5Service) Install(ctx context.Context, req L5InstallRequest) (*L5InstallResponse, error) {
	var resp L5InstallResponse
	if err := s.c.doWithRetry(ctx, http.MethodPost, "/v1/l5/install", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// L5UninstallRequest 卸 agent
type L5UninstallRequest struct {
	UserID    string `json:"user_id"`
	AgentName string `json:"agent_name"`
	Purge     bool   `json:"purge,omitempty"`
}

// L5UninstallResponse 卸 agent 响应
type L5UninstallResponse struct {
	OK            bool   `json:"ok"`
	UninstalledAt int64  `json:"uninstalled_at,omitempty"`
	SnapshotPath  string `json:"snapshot_path,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Uninstall 卸 agent(purge=true 全删,默认 false 保留 wau-profile 数据)。
//
// 对应 POST /v1/l5/uninstall。
func (s *L5Service) Uninstall(ctx context.Context, req L5UninstallRequest) (*L5UninstallResponse, error) {
	var resp L5UninstallResponse
	if err := s.c.doWithRetry(ctx, http.MethodPost, "/v1/l5/uninstall", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// L5UpdateRequest 更新 agent
type L5UpdateRequest struct {
	UserID        string `json:"user_id"`
	AgentName     string `json:"agent_name,omitempty"` // 空 = 全更新
	TargetVersion string `json:"target_version,omitempty"`
}

// L5UpdateResponse 更新 agent 响应
type L5UpdateResponse struct {
	OK            bool     `json:"ok"`
	UpdatedCount  int      `json:"updated_count,omitempty"`
	UpdatedAgents []string `json:"updated_agents,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// Update 更新 agent(agent_name="" = 全更新 per D72)。
//
// 对应 POST /v1/l5/update。
func (s *L5Service) Update(ctx context.Context, req L5UpdateRequest) (*L5UpdateResponse, error) {
	var resp L5UpdateResponse
	if err := s.c.doWithRetry(ctx, http.MethodPost, "/v1/l5/update", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// L5SearchRequest 搜 wau-registry
type L5SearchRequest struct {
	UserID   string `json:"user_id"`
	Query    string `json:"query"`
	Universe string `json:"universe,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// L5SearchResponse 搜响应
type L5SearchResponse struct {
	OK      bool          `json:"ok"`
	Results []L5SearchHit `json:"results,omitempty"`
	Total   int           `json:"total,omitempty"`
	Error   string        `json:"error,omitempty"`
}

// L5SearchHit 单条搜索结果(对齐 WAU-core-kernel L5SearchHit)
type L5SearchHit struct {
	Name        string  `json:"name"`
	Version     string  `json:"version"`
	Description string  `json:"description"`
	Author      string  `json:"author"`
	Universe    string  `json:"universe"`
	Homepage    string  `json:"homepage"`
	TrustScore  float64 `json:"trust_score"`
}

// Search 搜 wau-registry(类比 apt search / npm search)。
//
// 对应 POST /v1/l5/search。
func (s *L5Service) Search(ctx context.Context, req L5SearchRequest) (*L5SearchResponse, error) {
	var resp L5SearchResponse
	if err := s.c.doWithRetry(ctx, http.MethodPost, "/v1/l5/search", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// L5LoginRequest 登入 WAU 账户
type L5LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Endpoint string `json:"endpoint,omitempty"`
}

// L5LoginResponse 登入响应
type L5LoginResponse struct {
	OK           bool   `json:"ok"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Error        string `json:"error,omitempty"`
}

// Login 登入 WAU 账户(类比 docker login / npm login)。
//
// 对应 POST /v1/l5/login。
func (s *L5Service) Login(ctx context.Context, req L5LoginRequest) (*L5LoginResponse, error) {
	var resp L5LoginResponse
	if err := s.c.doWithRetry(ctx, http.MethodPost, "/v1/l5/login", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
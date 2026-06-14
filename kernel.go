package wau

import (
	"context"
	"net/http"
)

// KernelService 提供 kernel 元信息操作。
//
// **所有方法都走 Client.doWithRetry**,自动应用鉴权 + 熔断 + 重试。
type KernelService struct {
	c *Client
}

// Info returns kernel info (version, startTime, uptime, agent count, task count).
//
// 对应 GET /kernel/info。
func (s *KernelService) Info(ctx context.Context) (*KernelInfo, error) {
	var resp KernelInfo
	if err := s.c.doWithRetry(ctx, http.MethodGet, "/kernel/info", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Health checks kernel health (same as AgentsService.Health, exposed at top level).
//
// 对应 GET /health。
func (s *KernelService) Health(ctx context.Context) (*HealthResponse, error) {
	var resp HealthResponse
	if err := s.c.doWithRetry(ctx, http.MethodGet, "/health", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

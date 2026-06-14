package wau

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// AgentsService 提供 agent CRUD / 状态 / 评分 / 心跳 / 负载操作。
//
// 全部方法都接受 ctx(可取消),返回 typed error(用 errors.Is 判断)。
//
// **所有方法都走 Client.doWithRetry**,自动应用:
//   - HS256 鉴权(如果配置)
//   - 熔断(集成 wau-circuit)
//   - 重试(指数退避 + 抖动)
type AgentsService struct {
	c *Client
}

// Health checks kernel health.
//
// 对应 GET /health。
func (s *AgentsService) Health(ctx context.Context) (*HealthResponse, error) {
	var resp HealthResponse
	if err := s.c.doWithRetry(ctx, http.MethodGet, "/health", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// List 列出 agents(带可选过滤 + 分页)。
//
// 对应 GET /registry/agents?page=...&pageSize=...&skill=...&status=...&search=...
func (s *AgentsService) List(ctx context.Context, opts PageOptions) (*AgentListResponse, error) {
	q := url.Values{}
	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 10
	}
	q.Set("page", fmt.Sprintf("%d", opts.Page))
	q.Set("pageSize", fmt.Sprintf("%d", opts.PageSize))
	if opts.Skill != "" {
		q.Set("skill", opts.Skill)
	}
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	if opts.Search != "" {
		q.Set("search", opts.Search)
	}
	var resp AgentListResponse
	if err := s.c.doWithRetry(ctx, http.MethodGet, "/registry/agents?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Iter 提供迭代器(Go 1.23+ 泛型),遍历 List 全部页。
//
// 失败立刻返 error;成功遍历完所有页返 nil。
//
// 用法:
//
//	for agent, err := range s.Iter(ctx, PageOptions{Skill: "clinical"}) {
//	    if err != nil { ... }
//	    fmt.Println(agent.Name)
//	}
func (s *AgentsService) Iter(ctx context.Context, opts PageOptions) func(func(Agent, error) bool) {
	opts.Page = 1
	if opts.PageSize <= 0 {
		opts.PageSize = 10
	}
	return func(yield func(Agent, error) bool) {
		for {
			page, err := s.List(ctx, opts)
			if err != nil {
				yield(Agent{}, err)
				return
			}
			for _, a := range page.Agents {
				if !yield(a, nil) {
					return
				}
			}
			if opts.Page >= page.TotalPages {
				return
			}
			opts.Page++
		}
	}
}

// Get returns agent's comprehensive status.
//
// 对应 GET /registry/agents/{name}/status。
func (s *AgentsService) Get(ctx context.Context, name string) (*AgentStatus, error) {
	var resp AgentStatus
	if err := s.c.doWithRetry(ctx, http.MethodGet, fmt.Sprintf("/registry/agents/%s/status", name), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Score returns agent's score (5-dim subset, v0.6.0).
//
// 对应 GET /registry/agents/{name}/score。
func (s *AgentsService) Score(ctx context.Context, name string) (*AgentScore, error) {
	var resp AgentScore
	if err := s.c.doWithRetry(ctx, http.MethodGet, fmt.Sprintf("/registry/agents/%s/score", name), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Register registers a new agent (RBAC: trusted_agent / kernel_core).
//
// 对应 POST /registry/agents/register。
func (s *AgentsService) Register(ctx context.Context, req AgentRegisterRequest) error {
	return s.c.doWithRetry(ctx, http.MethodPost, "/registry/agents/register", req, nil)
}

// Deregister removes an agent by name (RBAC: trusted_agent / kernel_core).
//
// 对应 DELETE /registry/agents/{name}。
func (s *AgentsService) Deregister(ctx context.Context, name string) error {
	return s.c.doWithRetry(ctx, http.MethodDelete, fmt.Sprintf("/registry/agents/%s", name), nil, nil)
}

// Heartbeat reports agent heartbeat (agent-initiated).
//
// 对应 POST /registry/agents/heartbeat。
func (s *AgentsService) Heartbeat(ctx context.Context, agentID string) error {
	return s.c.doWithRetry(ctx, http.MethodPost, "/registry/agents/heartbeat", map[string]string{
		"agentId": agentID,
	}, nil)
}

// ReportLoad reports agent's runtime load.
//
// 对应 POST /heartbeat/load。
func (s *AgentsService) ReportLoad(ctx context.Context, agentID string, load AgentLoad) error {
	return s.c.doWithRetry(ctx, http.MethodPost, "/heartbeat/load", map[string]any{
		"agentId":     agentID,
		"activeTasks": load.ActiveTasks,
		"maxCapacity": load.MaxCapacity,
		"cpuUsage":    load.CPUUsage,
		"memoryUsage": load.MemoryUsage,
	}, nil)
}

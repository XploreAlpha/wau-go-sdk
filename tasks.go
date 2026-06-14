package wau

import (
	"context"
	"fmt"
	"net/http"
)

// TasksService 提供 task 提交 / 查询 / 模拟 操作。
//
// **所有方法都走 Client.doWithRetry**,自动应用鉴权 + 熔断 + 重试。
type TasksService struct {
	c *Client
}

// Submit submits a task for L4 processing (real A2A call).
//
// 对应 POST /registry/tasks/submit。
//
// v0.6.0 M3 关键修正:SubmitRequest 字段以 kernel 真相源为准({Prompt, TimeoutMs}),
// 不是 wau-cli 旧 DTO({Message, SourcePeer, ...})。
func (s *TasksService) Submit(ctx context.Context, req SubmitRequest) (*SubmitResponse, error) {
	var resp SubmitResponse
	if err := s.c.doWithRetry(ctx, http.MethodPost, "/registry/tasks/submit", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Simulate runs L3 decision only (no actual A2A call).
//
// 对应 POST /registry/tasks/simulate。
// 返回 DecisionInfo (不返 a2a_call_ms / response,因为没真发)。
func (s *TasksService) Simulate(ctx context.Context, req SubmitRequest) (*DecisionInfo, error) {
	var resp DecisionInfo
	if err := s.c.doWithRetry(ctx, http.MethodPost, "/registry/tasks/simulate", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Get returns task details by ID.
//
// 对应 GET /registry/tasks/{taskID}。
func (s *TasksService) Get(ctx context.Context, taskID string) (*Task, error) {
	var resp Task
	if err := s.c.doWithRetry(ctx, http.MethodGet, fmt.Sprintf("/registry/tasks/%s", taskID), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

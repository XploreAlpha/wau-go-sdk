package wau

import "context"

// IntentService 提供 intent 推荐 (gRPC 50053)。
//
// v0.6.0 M3 W5:P2 stub,所有方法返 ErrNotImplemented。
// v0.6.0 M3.1:实装 wau.intent.v1.IntentService 4 RPC (ParseIntent/RecommendAgent/ListAgents/HealthCheck)。
//
// 为什么不直接实现:本仓库本地 path buf generate 已跑通(2026-06-14),
// 但 gRPC client 还需要 @grpc/grpc-js / grpclib 依赖,而且 wau-intent-service
// 端到端集成需要更多联调时间。推到 M3.1 留口子。
type IntentService struct {
	c *Client
}

// Recommend 推荐 agent 给定 prompt(P2 stub)。
func (s *IntentService) Recommend(ctx context.Context, prompt string, topK int) (any, error) {
	_ = ctx
	_ = prompt
	_ = topK
	return nil, ErrNotImplemented
}

// ParseIntent 解析 intent 类型/技能(P2 stub)。
func (s *IntentService) ParseIntent(ctx context.Context, text string) (any, error) {
	_ = ctx
	_ = text
	return nil, ErrNotImplemented
}

// ListAgents 列出 agents(走 wau-intent-service 而非 kernel)。
func (s *IntentService) ListAgents(ctx context.Context, onlineOnly bool) (any, error) {
	_ = ctx
	_ = onlineOnly
	return nil, ErrNotImplemented
}

// HealthCheck 检查 wau-intent-service 健康。
func (s *IntentService) HealthCheck(ctx context.Context) (any, error) {
	_ = ctx
	return nil, ErrNotImplemented
}

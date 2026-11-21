package wau

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
)

// IntentService 提供 intent 解析 + agent 评分。
//
// ## 2026-11-19:从 gRPC stub 改成实装 HTTP
//
// 之前这个类型是 **P2 stub**,4 个方法全部返 ErrNotImplemented。
// M3.1 一直没做。
//
// 但 kernel 侧**早就有可用的 HTTP 端点**了(2026-11-19 对着生产服务器
// 100.125.139.145 实测确认):
//
//	POST /v1/intent/parse  {message, source_universe?}
//	    → 200 {"intent":{"type","required_skills","urgency","estimated_complexity","source_universe"}}
//
//	POST /v1/intent/score  {agents[], required_skills[], intent_type, urgency, source_universe?}
//	    → 200 {"scores":[{"AgentID","TotalScore","Dimensions":{...},"Rank"}]}
//
// ⇒ **不必等 gRPC**,直接走 HTTP 就能把 intent 服务变成可用的。
//
// ## 形状上的坑(实测)
//
// /v1/intent/score 返回的 score 元素用的是 **Go 字段名**
// (AgentID / TotalScore / Dimensions),不是 snake_case
// —— 因为 internal/scoring.AgentScore 这个 struct **没有 json tag**。
// 而 /registry/agents/{name}/score 返回的是 camelCase
// (agentId / totalScore / dimensions)⇒ 两个端点形状不一致,两种都认。
type IntentService struct {
	c *Client
}

// IntentEntry 是 POST /v1/intent/parse 的解析结果。
//
// 字段名跟 kernel 的 proxy.IntentEntry 1:1(snake_case)。
type IntentEntry struct {
	Type                string   `json:"type"`
	RequiredSkills      []string `json:"required_skills"`
	Urgency             string   `json:"urgency"`
	EstimatedComplexity int      `json:"estimated_complexity"`
	SourceUniverse      string   `json:"source_universe,omitempty"`
}

// IntentAgentScore 是 POST /v1/intent/score 里一个 agent 的评分。
//
// kernel 返回 Go 字段名(AgentID/TotalScore/Dimensions/Rank),两种拼写都认。
type IntentAgentScore struct {
	AgentID    string             `json:"agentId"`
	TotalScore float64            `json:"totalScore"`
	Dimensions map[string]float64 `json:"dimensions,omitempty"`
	Rank       int                `json:"rank,omitempty"`
}

// UnmarshalJSON 兼容 Go 字段名与 camelCase 两套拼写。
func (s *IntentAgentScore) UnmarshalJSON(data []byte) error {
	var raw struct {
		AgentID    string             `json:"AgentID"`
		AgentIDC   string             `json:"agentId"`
		TotalScore float64            `json:"TotalScore"`
		TotalScoreC float64            `json:"totalScore"`
		Dimensions map[string]float64 `json:"Dimensions"`
		DimsC      map[string]float64 `json:"dimensions"`
		Rank       int                `json:"Rank"`
		RankC      int                `json:"rank"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.AgentID = raw.AgentID
	if s.AgentID == "" {
		s.AgentID = raw.AgentIDC
	}
	s.TotalScore = raw.TotalScore
	if s.TotalScore == 0 {
		s.TotalScore = raw.TotalScoreC
	}
	s.Dimensions = raw.Dimensions
	if len(s.Dimensions) == 0 {
		s.Dimensions = raw.DimsC
	}
	s.Rank = raw.Rank
	if s.Rank == 0 {
		s.Rank = raw.RankC
	}
	return nil
}

// ParseIntent 解析用户输入的 intent。
//
// 对应 POST /v1/intent/parse。
func (s *IntentService) ParseIntent(ctx context.Context, text string) (*IntentEntry, error) {
	return s.ParseIntentFrom(ctx, text, "")
}

// ParseIntentFrom 带 source_universe 的 intent 解析。
func (s *IntentService) ParseIntentFrom(ctx context.Context, text, sourceUniverse string) (*IntentEntry, error) {
	body := map[string]string{"message": text}
	if sourceUniverse != "" {
		body["source_universe"] = sourceUniverse
	}
	var raw struct {
		Intent IntentEntry `json:"intent"`
	}
	if err := s.c.doWithRetry(ctx, http.MethodPost, "/v1/intent/parse", body, &raw); err != nil {
		return nil, err
	}
	entry := raw.Intent
	if entry.Type == "" {
		// 兼容直接返回 intent 字段(没包一层)的情况。
		var flat IntentEntry
		if err := s.c.doWithRetry(ctx, http.MethodPost, "/v1/intent/parse", body, &flat); err == nil {
			entry = flat
		}
	}
	if entry.RequiredSkills == nil {
		entry.RequiredSkills = []string{}
	}
	return &entry, nil
}

// ScoreAgents 给 agent 列表打分(按 Rank 升序)。
//
// 对应 POST /v1/intent/score。
func (s *IntentService) ScoreAgents(
	ctx context.Context,
	agents []string,
	requiredSkills []string,
	intentType string,
	urgency string,
	sourceUniverse string,
) ([]IntentAgentScore, error) {
	body := map[string]any{"agents": agents}
	if len(requiredSkills) > 0 {
		body["required_skills"] = requiredSkills
	}
	if intentType != "" {
		body["intent_type"] = intentType
	}
	if urgency != "" {
		body["urgency"] = urgency
	}
	if sourceUniverse != "" {
		body["source_universe"] = sourceUniverse
	}
	var raw struct {
		Scores []IntentAgentScore `json:"scores"`
	}
	if err := s.c.doWithRetry(ctx, http.MethodPost, "/v1/intent/score", body, &raw); err != nil {
		return nil, err
	}
	scores := raw.Scores
	sort.SliceStable(scores, func(i, j int) bool {
		ri, rj := scores[i].Rank, scores[j].Rank
		if ri == 0 {
			ri = 1 << 30
		}
		if rj == 0 {
			rj = 1 << 30
		}
		if ri != rj {
			return ri < rj
		}
		return scores[i].TotalScore > scores[j].TotalScore
	})
	return scores, nil
}

// Recommend 推荐最合适的 agent。
//
// 流程:ParseIntent → ListAgents 拿候选 → ScoreAgents 打分 → 取 topK。
func (s *IntentService) Recommend(ctx context.Context, prompt string, topK int) ([]IntentAgentScore, error) {
	entry, err := s.ParseIntent(ctx, prompt)
	if err != nil {
		return nil, err
	}
	agents, err := s.ListAgents(ctx, true)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	scores, err := s.ScoreAgents(ctx, names, entry.RequiredSkills, entry.Type, entry.Urgency, entry.SourceUniverse)
	if err != nil {
		return nil, err
	}
	if topK <= 0 {
		topK = 1
	}
	if len(scores) > topK {
		scores = scores[:topK]
	}
	return scores, nil
}

// ListAgents 列出 agents。
//
// 对应 GET /registry/agents(kernel 返回**裸数组**,AgentListResponse 已兼容)。
func (s *IntentService) ListAgents(ctx context.Context, onlineOnly bool) ([]Agent, error) {
	var resp AgentListResponse
	if err := s.c.doWithRetry(ctx, http.MethodGet, "/registry/agents", nil, &resp); err != nil {
		return nil, err
	}
	if !onlineOnly {
		return resp.Agents, nil
	}
	out := make([]Agent, 0, len(resp.Agents))
	for _, a := range resp.Agents {
		if a.Name != "" {
			out = append(out, a)
		}
	}
	return out, nil
}

// HealthCheck 检查 kernel 健康。
//
// 对应 GET /health。
func (s *IntentService) HealthCheck(ctx context.Context) (*HealthResponse, error) {
	var resp HealthResponse
	if err := s.c.doWithRetry(ctx, http.MethodGet, "/health", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

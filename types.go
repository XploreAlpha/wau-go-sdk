// Package wau provides the official Go SDK for WAU-core-kernel.
//
// v0.6.0 M3 W5 — 抽取自 wau-cli/internal/client/(2026-06-13)
// 改进:typed errors / 熔断 / 重试 / HS256 鉴权 / SubmitRequest 字段以 kernel 真相源为准
package wau

// HealthResponse is the response from GET /health.
type HealthResponse struct {
	Status  string  `json:"status"`
	Version string  `json:"version"`
	Uptime  float64 `json:"uptime"`
	Redis   string  `json:"redis"`
	Error   string  `json:"error,omitempty"`
}

// KernelInfo is the response from GET /kernel/info.
type KernelInfo struct {
	Version     string `json:"version"`
	StartTime   string `json:"startTime"`
	Uptime      int64  `json:"uptime"`
	AgentsCount int    `json:"agentsCount"`
	TasksCount  int    `json:"tasksCount"`
}

// Agent represents a registered WAU agent.
type Agent struct {
	Name        string   `json:"name"`
	ID          string   `json:"id"`
	URL         string   `json:"url"`
	Description string   `json:"description"`
	Skills      []string `json:"skills"`
	Universes   []string `json:"universes"`
	Trust       float64  `json:"trust"`
	Status      string   `json:"status"`
	LastSeen    string   `json:"lastSeen"`
}

// AgentListResponse is the paginated list of agents.
type AgentListResponse struct {
	Agents     []Agent `json:"agents"`
	Total      int64   `json:"total"`
	Page       int     `json:"page"`
	PageSize   int     `json:"pageSize"`
	TotalPages int     `json:"totalPages"`
}

// PageOptions paginates Agent list queries.
type PageOptions struct {
	Page     int    // 1-based; default 1
	PageSize int    // default 10, max 100
	Skill    string // optional filter
	Status   string // optional filter
	Search   string // optional fuzzy match
}

// PageResult is the generic paginated result.
type PageResult[T any] struct {
	Items      []T
	Total      int64
	Page       int
	PageSize   int
	TotalPages int
}

// AgentRegisterRequest registers a new agent.
type AgentRegisterRequest struct {
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Description string   `json:"description"`
	Skills      []string `json:"skills"`
	Universes   []string `json:"universes"`
}

// AgentScore represents an agent's 15-dim score (current kernel returns 5-dim subset).
type AgentScore struct {
	Name        string  `json:"name"`
	TotalScore  float64 `json:"totalScore"`
	TrustScore  float64 `json:"trustScore"`
	SkillMatch  float64 `json:"skillMatch"`
	HealthScore float64 `json:"healthScore"`
	LoadScore   float64 `json:"loadScore"`
}

// AgentLoad represents agent runtime load (nested in AgentStatus).
type AgentLoad struct {
	ActiveTasks int     `json:"activeTasks"`
	MaxCapacity int     `json:"maxCapacity"`
	CPUUsage    float64 `json:"cpuUsage"`
	MemoryUsage float64 `json:"memoryUsage"`
}

// AgentStatus represents agent's comprehensive status.
type AgentStatus struct {
	Name    string    `json:"name"`
	Status  string    `json:"status"`
	Trust   float64   `json:"trust"`
	Load    AgentLoad `json:"load"`
	Circuit string    `json:"circuit"`
}

// Task represents a task record.
type Task struct {
	TaskID         string   `json:"taskId"`
	Message        string   `json:"message"`
	SourcePeer     string   `json:"sourcePeer"`
	SourceAgentID  string   `json:"sourceAgentId,omitempty"`
	Status         string   `json:"status"`
	AssignedAgent  string   `json:"assignedAgent,omitempty"`
	Result         string   `json:"result,omitempty"`
	CreatedAt      int64    `json:"createdAt"`
	UpdatedAt      int64    `json:"updatedAt"`
	RequiredSkills []string `json:"requiredSkills,omitempty"`
}

// SubmitRequest is the L4 submit request.
//
// v0.6.0 M3 关键修正:wau-cli 旧 DTO 用 {Message, SourcePeer, SourceAgentID, Intent},
// 跟 kernel 端 SubmitRequest{Prompt, TimeoutMs} 字段不一致,导致 wau-cli 实际调 L4
// 大概率失败(binding:"required" 拦截)。SDK 以 kernel 真相源为准。
//
// 参考:[handle_submit_l4.go:80-83](https://github.com/wau/WAU-core-kernel/blob/main/cmd/wau-core/handle_submit_l4.go#L80-L83)
type SubmitRequest struct {
	Prompt    string `json:"prompt" binding:"required"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

// DecisionInfo is the L3 decision details (nested in SubmitResponse).
type DecisionInfo struct {
	SelectedAgent  string      `json:"selected_agent"`
	Score          float64     `json:"score"`
	DecisionTimeMs int         `json:"decision_time_ms"`
	Candidates     []Candidate `json:"candidates,omitempty"`
}

// Candidate is one agent in decision candidates.
type Candidate struct {
	Name   string  `json:"name"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// SubmitResponse is the L4 submit response (kernel v0.5.0+).
type SubmitResponse struct {
	TaskID        string       `json:"task_id"`
	AgentID       string       `json:"agent_id,omitempty"`
	AgentURL      string       `json:"agent_url,omitempty"`
	Score         float64      `json:"score,omitempty"`
	Dimensions    map[string]float64 `json:"dimensions,omitempty"`
	Decision      DecisionInfo `json:"decision"`
	Status        string       `json:"status"`
	SelectedAgent string       `json:"selected_agent,omitempty"`
	A2ACallMs     int          `json:"a2a_call_ms,omitempty"`
	Response      any          `json:"response,omitempty"`
	Error         string       `json:"error,omitempty"`
	SourcePeer    string       `json:"source_peer,omitempty"`
	SourceAgentID string       `json:"source_agent_id,omitempty"`
}

// IntentDTO is the optional intent hint (L3).
type IntentDTO struct {
	Type                string   `json:"type"`
	RequiredSkills      []string `json:"requiredSkills"`
	Urgency             string   `json:"urgency"`
	EstimatedComplexity int      `json:"estimatedComplexity"`
}

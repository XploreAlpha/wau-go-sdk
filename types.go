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
	// UniverseLabels K8s-style labels(per universe,v0.8.0 M3-2A 新增)
	//   - 业务分组用 Universes(原字段,保持向后兼容)
	//   - 资源 / 调度特征用 UniverseLabels(新字段,per agent 粒度)
	//   - 老 client 不传 → nil(server 视为空 map)
	//   - 字段名跟 afp-protocol v0.2 + WAU-core-kernel proto 1:1 对齐
	UniverseLabels map[string]string `json:"universe_labels,omitempty"`
	Trust          float64           `json:"trust"`
	Status         string            `json:"status"`
	LastSeen       string            `json:"lastSeen"`
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
	// UniverseLabels K8s-style labels(per universe,v0.8.0 M3-2A 新增)
	// 跟 Agent.UniverseLabels 字段语义一致(per agent 粒度)
	UniverseLabels map[string]string `json:"universe_labels,omitempty"`
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

// ============== Chat / LLM DTO(v0.9.0 M3 §3.7 新增,per D20 architecture-pivot)==============
//
// wau-edge OpenAI 兼容层转发路径(per M2 §2.5):
//   bot → wau-edge :18402 /v1/chat/completions
//        → wau-llm-router :18403 /v1/resolve(决定 userToken + model)
//        → new-api sidecar :3000 /v1/chat/completions(真 LLM 调用)
//
// 字段 1:1 对齐 OpenAI Chat Completions API(per https://platform.openai.com/docs/api-reference/chat),
// 4 SDK 通用,test mock 跟真 wau-edge 字节级兼容。

// ChatMessage is one message in a chat conversation.
//
// Role: "system" / "user" / "assistant" / "tool"
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Name 可选(per OpenAI spec)
	Name string `json:"name,omitempty"`
}

// ChatCompletionRequest is the OpenAI-compatible chat request.
//
// Model: 必填(如 "gpt-4o-mini" / "claude-haiku"),为空时 wau-edge 走 default_model。
// Messages: 必填 ≥ 1 条 user 消息。
// Stream: 雏形期只支持 false(M3 §3.7 续支持 streaming)。
// Universe: 业务分组(透传到 wau-llm-router + new-api),非必填,默认 "default"。
// Tenant: 由 JWT claim 注入,request 体不重复带(server 端以 JWT 为准,防越权)。
type ChatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
	// WAU 扩展字段(per D20) — Universe 业务分组
	Universe string `json:"universe,omitempty"`
	// 自由扩展,4 SDK 通用 metadata 通道(走 OpenAI 不识别字段,wau-edge 透传到 router)
	Metadata map[string]string `json:"metadata,omitempty"`
	// 常用可选(对齐 OpenAI spec,雏形期不强制实现)
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
}

// ChatChoice one of N returned choices (OpenAI compat).
type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatUsage token usage stats (OpenAI compat).
type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionResponse OpenAI 兼容的 chat response。
//
// 字段 1:1 对齐 OpenAI ChatCompletion object;wau-edge 跟 wau-llm-router / new-api
// 串联后字节级兼容(per M2 §2.5 端到端 mock 验证)。
type ChatCompletionResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   ChatUsage    `json:"usage"`
	// WAU 扩展 — reason 是 wau-llm-router 的决策原因,debug / audit 用
	Reason string `json:"reason,omitempty"`
	// WAU 扩展 (Stage 3.1 #11, 2026-07-03) — provider 是 wau-llm-router Resolve 选中的
	// LLM provider 名称(如 "deepseek-v4-flash" / "gpt-4o-mini" / "claude-haiku-4-5"),
	// 透传自 wau-store 真相源,debug / audit / 成本归因用。
	// 老调用方不读 → 无影响(omitempty 字段,缺省空串)。
	Provider string `json:"provider,omitempty"`
}

// ============== Streaming SSE types(per Stage 3.1 #10, 2026-07-02)==============
//
// OpenAI ChatCompletionChunk 协议 1:1 对齐(per https://platform.openai.com/docs/api-reference/chat-streaming)。
// 4 SDK 通用字段(per Stage 0 4 SDK 5/5 字段对齐)。
//
// 完整链路:SDK → wau-edge :18402 /v1/chat/completions?stream=true
//                  → wau-llm-router :18404 Resolve(unary, 拿 userToken + model)
//                  → new-api sidecar :3000 /v1/chat/completions?stream=true
//                  → DeepSeek v4-flash reasoning model → SSE chunks
//                  → 响应回 wau-edge → 编码 SSE → 响应回 SDK

// ChunkDelta 是 OpenAI ChatCompletionChunk.choices[].delta 对象。
//
// Role 字段只在首 chunk 有值("assistant"),omitempty 保证其他 chunk 不出 role 字段。
// Content 是增量字符流(wau-edge 7 chunks 验证 per C.1:"1" → "+" → "1" → "=" → "2")。
type ChunkDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// ChunkChoice 是 OpenAI ChatCompletionChunk.choices[] 元素。
//
// FinishReason 字段在流中间为 null(per OpenAI 协议),结束 chunk 为 "stop" / "length"。
// 用 *string 指针类型 + omitempty,序列化为 null 而非空串,严格对齐 OpenAI spec。
type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

// ChatCompletionChunk 是 OpenAI ChatCompletion streaming 响应的一个 chunk。
//
// wau-edge handler.go handleStream (L204-273) 编码这种格式,SSE 包装为:
//   data: {<JSON>}\n\n
//
// 终止标志:data: [DONE]\n\n(per stream.go WriteDone)
type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"` // 固定 "chat.completion.chunk"
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
}

// ────────────────────────────────────────────────────────────
// WauWorkflow(per v1.0.1 SoT doc + SDK Consumer Contract §二.2 + D78 byte-equal)
// TS canonical: src/wau/types.ts#L116-L158
// ────────────────────────────────────────────────────────────

// WauWorkflowType 是 WauWorkflow 的 workflow 类型 enum(6 值,per WauWorkflow msg type spec §三.3)
// 跟 wau-intent / afp-protocol proto WorkflowType 一致
// 5 SDK byte-equal,wire format 是 snake_case uppercase 字符串
type WauWorkflowType string

const (
	WauWorkflowTypeUnspecified WauWorkflowType = "WORKFLOW_TYPE_UNSPECIFIED"
	WauWorkflowTypeSingle      WauWorkflowType = "WORKFLOW_TYPE_SINGLE"
	WauWorkflowTypeChain       WauWorkflowType = "WORKFLOW_TYPE_CHAIN"
	WauWorkflowTypeParallel    WauWorkflowType = "WORKFLOW_TYPE_PARALLEL"
	WauWorkflowTypeQuorum      WauWorkflowType = "WORKFLOW_TYPE_QUORUM"
	WauWorkflowTypeFanOut      WauWorkflowType = "WORKFLOW_TYPE_FAN_OUT"
)

// WauWorkflowAgent 是 WauWorkflow 里单个 agent 推荐块(per TS L49-L55)
// 跟 wau-intent proto WauWorkflowAgent 字段 1:1
type WauWorkflowAgent struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Skills     []string `json:"skills"`
	Confidence float64  `json:"confidence"`
}

// WauWorkflowDependency 是 DAG 依赖图节点(per TS L60-L62)
// 跟 wau-intent proto WauWorkflowDependency 字段 1:1
type WauWorkflowDependency struct {
	UpstreamAgents []string `json:"upstream_agents"`
}

// WauWorkflowDependencyGraph 是 WauWorkflow.dependency_graph 嵌套结构(per TS L119-L121)
type WauWorkflowDependencyGraph struct {
	Dependencies map[string]WauWorkflowDependency `json:"dependencies"`
}

// WauWorkflow 是 5 SDK 共享 wire format 的核心 DTO(per SDK Consumer Contract §二.2)
//
// 19 字段(5 必填 + 14 元数据):
//   - 必填 5 字段:agents / dependency_graph / confidence / workflow_type / harness
//   - 标识 3 字段:workflow_id / created_at / user_id
//   - DAG pattern 元数据 3 optional:dag_pattern_hint / description / estimated_duration_ms
//   - 推荐上下文 3 字段:original_query / parent_workflow_id? / retry_count?
//   - Server metadata 3 字段:server_version / trace_id / ttl_ms
//   - 鉴权 2 字段:auth_user_id / auth_claim_set
//
// JSON 字段 snake_case(per TS L15 + WauWorkflow msg type spec §三.3 + #14 A 拍板)
//
// ⚠️ voice workflow 必须 harness='codex-appserver'(per #17 配错保护)
type WauWorkflow struct {
	// === 必填 5 字段 ===
	Agents          []WauWorkflowAgent         `json:"agents"`
	DependencyGraph WauWorkflowDependencyGraph `json:"dependency_graph"`
	Confidence      float64                    `json:"confidence"`
	WorkflowType    WauWorkflowType            `json:"workflow_type"`
	/** voice workflow 必须 'codex-appserver',其它 harness 抛错(per #17) */
	Harness string `json:"harness"`

	// === 标识字段 ===
	WorkflowID string `json:"workflow_id"`
	/** unix ms */
	CreatedAt int64  `json:"created_at"`
	UserID    string `json:"user_id"`

	// === DAG pattern 元数据(per #4 抽象 wau-dag-patterns) ===
	DagPatternHint      *string `json:"dag_pattern_hint,omitempty"`
	Description         *string `json:"description,omitempty"`
	EstimatedDurationMs *int64  `json:"estimated_duration_ms,omitempty"`

	// === 推荐上下文 ===
	/** 用户原始 query */
	OriginalQuery string  `json:"original_query"`
	/** 子 workflow 追溯 */
	ParentWorkflowID *string `json:"parent_workflow_id,omitempty"`
	/** 0=首次,1+=重试 */
	RetryCount *int `json:"retry_count,omitempty"`

	// === Server-side metadata ===
	/** wau-intent server version,byte-equal verify anchor */
	ServerVersion string `json:"server_version"`
	/** 跨 SDK 调试 trace */
	TraceID string `json:"trace_id"`
	/** workflow 有效期,过期 client 拒收 */
	TTLMs int64 `json:"ttl_ms"`

	// === 鉴权上下文(per D66=B JWT 4-claim) ===
	AuthUserID string `json:"auth_user_id"`
	/** 4 claim names:sub/aud/exp/scope */
	AuthClaimSet []string `json:"auth_claim_set"`
}

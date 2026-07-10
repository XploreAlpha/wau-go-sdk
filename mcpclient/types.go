package mcpclient

// ────────────────────────────────────────────────────────
// 7 sync tool DTO(per kernel mcp.responseToMap/taskToMap 等 byte-equal)
// ────────────────────────────────────────────────────────

// AgentRef 是 MCP "target" 字段的简化表达(跟 kernel mcp.Handler.extractTarget 对齐)。
//
// target 在 JSON-RPC params 里可以是 string("Fox")或 object {"name":"Fox",...}。
// 本 type 只用在 typed method(SendMessage / GetTask 等)— 用 string 简写时
// 由 SDK 内部包成 map[string]any。
type AgentRef struct {
	Name     string   `json:"name,omitempty"`
	URL      string   `json:"url,omitempty"`
	Universe string   `json:"universe,omitempty"`
	Tenant   string   `json:"tenant,omitempty"`
	Skills   []string `json:"skills,omitempty"`
}

// Part 是 Message 内容片段(对齐 protocol.Part)。
type Part struct {
	Kind      string `json:"kind,omitempty"`
	Text      string `json:"text,omitempty"`
	URL       string `json:"url,omitempty"`
	Data      any    `json:"data,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Filename  string `json:"filename,omitempty"`
}

// Message 是协议消息体(对齐 protocol.Message,subset)。
type Message struct {
	MessageID string `json:"message_id,omitempty"`
	Role      string `json:"role,omitempty"` // "user" | "agent"
	ContextID string `json:"context_id,omitempty"`
	Parts     []Part `json:"parts,omitempty"`
}

// TaskStatus 是任务状态(对齐 protocol.TaskStatus)。
type TaskStatus struct {
	State     string   `json:"state,omitempty"`
	Message   *Message `json:"message,omitempty"`
	Timestamp string   `json:"timestamp,omitempty"`
}

// Task 是异步任务句柄(对齐 protocol.Task,DTO 子集)。
type Task struct {
	TaskID    string         `json:"task_id,omitempty"`
	ContextID string         `json:"context_id,omitempty"`
	Status    TaskStatus     `json:"status,omitempty"`
	History   []Message      `json:"history,omitempty"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Artifact 是 agent 产出(对齐 protocol.Artifact DTO)。
type Artifact struct {
	ArtifactID  string `json:"artifact_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Parts       []Part `json:"parts,omitempty"`
}

// Response 是 SendMessage 返的 DTO(对齐 protocol.Response)。
type Response struct {
	Kind      string     `json:"kind,omitempty"`
	Message   *Message   `json:"message,omitempty"`
	Task      *Task      `json:"task,omitempty"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

// AgentCard 是 ParseAgentCard / GetExtendedAgentCard 返的 DTO。
//
// 本 SDK 端的 AgentCard 是 kernel 端 protocol.AgentCard 的简化版,
// 只暴露 SDK caller 直接用得到的字段;完整字段透传走 *ExtendedAgentCard。
type AgentCard struct {
	Name                string   `json:"name,omitempty"`
	Description         string   `json:"description,omitempty"`
	URL                 string   `json:"url,omitempty"`
	Version             string   `json:"version,omitempty"`
	Skills              []string `json:"skills,omitempty"`
	DefaultInputModes   []string `json:"default_input_modes,omitempty"`
	DefaultOutputModes  []string `json:"default_output_modes,omitempty"`
	SupportedInterfaces []string `json:"supported_interfaces,omitempty"`
}

// ExtendedAgentCard 是 GetExtendedAgentCard 返的扩展版(含 capabilities)。
type ExtendedAgentCard struct {
	AgentCard
	Capabilities map[string]any `json:"capabilities,omitempty"`
}

// TaskFilter 是 ListTasks filter DTO(对齐 protocol.TaskFilter)。
type TaskFilter struct {
	State     string `json:"state,omitempty"`
	ContextID string `json:"context_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

// ListTasksResult 是 ListTasks 返的包装(对齐 handler.handleListTasks response)。
type ListTasksResult struct {
	Tasks []Task `json:"tasks"`
	Total int    `json:"total"`
}

// PushNotificationConfig 是 CreateTaskPushNotificationConfig 的 config DTO
// (对齐 protocol.PushConfig,W4 kernel 实装后才生效)。
type PushNotificationConfig struct {
	URL     string            `json:"url,omitempty"`
	Token   string            `json:"token,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

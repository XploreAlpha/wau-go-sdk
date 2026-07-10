package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
)

// Tool name 常量(对齐 kernel mcp/server.go ToolXxx + handler routeToProtocol)。
//
// 公开:SDK caller 可以用 const 拼 params,也可用 typed wrapper method。
const (
	ToolHealthCheck                      = "health_check"
	ToolParseAgentCard                   = "parse_agent_card"
	ToolSendMessage                      = "send_message"
	ToolStreamMessage                    = "stream_message"
	ToolGetTask                          = "get_task"
	ToolListTasks                        = "list_tasks"
	ToolCancelTask                       = "cancel_task"
	ToolSubscribeToTask                  = "subscribe_to_task"
	ToolCreateTaskPushNotificationConfig = "create_task_push_notification_config"
	ToolGetExtendedAgentCard             = "get_extended_agent_card"
)

// ────────────────────────────────────────────────────────
// 7 sync tool wrapper(W3 D87.1 实装)
// ────────────────────────────────────────────────────────

// HealthCheck 调 health_check tool。
//
// params:
//   - target: string (agent name) 或 AgentRef object
//
// 返回 {"status": "ok", "target": {...}} map,也可传 *map[string]any 直接拿 raw。
func (c *Client) HealthCheck(ctx context.Context, target any) (map[string]any, error) {
	params := map[string]any{
		"name":   ToolHealthCheck,
		"target": normalizeTarget(target),
	}
	var out map[string]any
	if err := c.call(ctx, "tools/call", params, false, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ParseAgentCard 调 parse_agent_card tool。
//
// raw 可以是 string(JSON)、[]byte(JSON)、或 map[string]any(已 unmarshal 的 object)。
// 跟 kernel handler.handleParseAgentCard 支持的三种 input 类型对齐。
func (c *Client) ParseAgentCard(ctx context.Context, raw any) (*AgentCard, error) {
	params, err := buildParseAgentCardParams(raw)
	if err != nil {
		return nil, err
	}
	var card AgentCard
	if err := c.call(ctx, "tools/call", params, false, &card); err != nil {
		return nil, err
	}
	return &card, nil
}

// SendMessage 调 send_message tool。
//
// params:
//   - target: string 或 AgentRef
//   - message: *Message(必填 Role + 至少 1 个 Part)
//
// 返回 *Response(对齐 kernel handler.responseToMap)。
func (c *Client) SendMessage(ctx context.Context, target any, msg *Message) (*Response, error) {
	if msg == nil {
		return nil, fmt.Errorf("mcpclient: message is required")
	}
	if len(msg.Parts) == 0 {
		return nil, fmt.Errorf("mcpclient: message.parts must have at least 1 item")
	}
	params := map[string]any{
		"name":    ToolSendMessage,
		"target":  normalizeTarget(target),
		"message": msg,
	}
	var resp Response
	if err := c.call(ctx, "tools/call", params, false, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetTask 调 get_task tool。
func (c *Client) GetTask(ctx context.Context, target any, taskID string) (*Task, error) {
	if taskID == "" {
		return nil, fmt.Errorf("mcpclient: task_id is required")
	}
	params := map[string]any{
		"name":    ToolGetTask,
		"target":  normalizeTarget(target),
		"task_id": taskID,
	}
	var task Task
	if err := c.call(ctx, "tools/call", params, false, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// ListTasks 调 list_tasks tool。
//
// filter 可为 nil(列所有)。
func (c *Client) ListTasks(ctx context.Context, target any, filter *TaskFilter) (*ListTasksResult, error) {
	params := map[string]any{
		"name":   ToolListTasks,
		"target": normalizeTarget(target),
	}
	if filter != nil {
		params["filter"] = filter
	}
	var result ListTasksResult
	if err := c.call(ctx, "tools/call", params, false, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CancelTask 调 cancel_task tool。
//
// 返回 canceled Task(对齐 kernel handler.handleCancelTask)。
func (c *Client) CancelTask(ctx context.Context, target any, taskID string) (*Task, error) {
	if taskID == "" {
		return nil, fmt.Errorf("mcpclient: task_id is required")
	}
	params := map[string]any{
		"name":    ToolCancelTask,
		"target":  normalizeTarget(target),
		"task_id": taskID,
	}
	var task Task
	if err := c.call(ctx, "tools/call", params, false, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// GetExtendedAgentCard 调 get_extended_agent_card tool。
//
// 返回 ExtendedAgentCard(含 capabilities map),跟普通 AgentCard 的差别:
// 多了 Capabilities 字段(capability 描述)。
func (c *Client) GetExtendedAgentCard(ctx context.Context, target any) (*ExtendedAgentCard, error) {
	params := map[string]any{
		"name":   ToolGetExtendedAgentCard,
		"target": normalizeTarget(target),
	}
	var card ExtendedAgentCard
	if err := c.call(ctx, "tools/call", params, false, &card); err != nil {
		return nil, err
	}
	return &card, nil
}

// ────────────────────────────────────────────────────────
// 3 deferred method placeholder(W4 notification + W5 SSE)
//
// 本 commit 只占位,等 kernel W4/W5 实装后再加 wire format 逻辑。
// ────────────────────────────────────────────────────────

// CreateTaskPushNotificationConfig 调 create_task_push_notification_config tool(W4 实装)。
func (c *Client) CreateTaskPushNotificationConfig(ctx context.Context, target any, cfg *PushNotificationConfig) (*PushNotificationConfig, error) {
	return nil, fmt.Errorf("mcpclient: CreateTaskPushNotificationConfig deferred to W4 (kernel 待实装)")
}

// StreamMessage 调 stream_message tool(W5 实装,SSE)。
//
// W5 实现后返 channel 接收 stream event;当前返 error。
func (c *Client) StreamMessage(ctx context.Context, target any, msg *Message) error {
	return fmt.Errorf("mcpclient: StreamMessage deferred to W5 (kernel SSE 待实装)")
}

// SubscribeToTask 调 subscribe_to_task tool(W5 实装,SSE)。
func (c *Client) SubscribeToTask(ctx context.Context, target any, taskID string) error {
	return fmt.Errorf("mcpclient: SubscribeToTask deferred to W5 (kernel SSE 待实装)")
}

// ────────────────────────────────────────────────────────
// Internal helpers
// ────────────────────────────────────────────────────────

// normalizeTarget 把 target 归一化成 JSON-RPC params 里能直接用的值。
//
// 接受:
//   - string:"Fox" → string
//   - *AgentRef   → map[string]any
//   - AgentRef    → map[string]any
//
// 跟 kernel mcp.Handler.extractTarget 的 (string | map[string]any) 输入对齐。
func normalizeTarget(target any) any {
	switch v := target.(type) {
	case nil:
		return nil
	case string:
		if v == "" {
			return nil
		}
		return v
	case *AgentRef:
		if v == nil {
			return nil
		}
		return v
	case AgentRef:
		return v
	default:
		// 已 unmarshal 的 map 等 — 让 JSON-RPC 直接传
		return target
	}
}

// buildParseAgentCardParams 把 raw 构造成 kernel handler 期望的 params 格式。
//
// kernel handler 支持 string / []byte / map[string]any 三种 raw 类型;
// 我们这里同样支持,JSON-RPC transport 走 map 序列化。
func buildParseAgentCardParams(raw any) (map[string]any, error) {
	if raw == nil {
		return nil, fmt.Errorf("mcpclient: raw is required")
	}
	var rawValue any
	switch v := raw.(type) {
	case string:
		// 字符串 — 当作 JSON literal 传,让 server 端 unmarshal
		// (handler.handleParseAgentCard 接受 string)
		rawValue = v
	case []byte:
		// bytes — 同样当 JSON 字符串
		rawValue = string(v)
	case map[string]any:
		// 已 unmarshal 的 object — 直接传
		rawValue = v
	default:
		// fallback: marshal 成 JSON 再传(让 server handler 走 map[string]any 路径)
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("mcpclient: marshal raw: %w", err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("mcpclient: raw not JSON-serializable: %w", err)
		}
		rawValue = m
	}
	return map[string]any{
		"name": ToolParseAgentCard,
		"raw":  rawValue,
	}, nil
}

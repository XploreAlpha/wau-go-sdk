package wau

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// A2A v1.0 客户端 —— 调外部 agent。
//
// ## 2026-11-19 新增
//
// 5 个 SDK 之前都没有 A2A 客户端 —— 只有 MCP 的 send_message tool
// (那走 MCP,不是 A2A)。而 kernel 已经在用 A2A v1.0 调外部 agent 了
// (ZeroClaw / Hermes / 边缘 agent)⇒ SDK 侧要能直连 A2A 才能自己组网。
//
// ## A2A v1.0 的线格式(实测,对着 hermes-shop01:9901)
//
// 请求:
//
//	POST {agent_url}
//	Content-Type: application/json
//	Accept: application/json
//	A2A-Version: 1.0              ← ★ 必带,缺失或值不匹配服务端直接拒
//	Authorization: Bearer <token>  ← 可选(agent 开 pairing 认证时必带)
//
//	{"jsonrpc":"2.0","id":"<uuid>","method":"SendMessage",
//	 "params":{"message":{"messageId":"<uuid>","role":"user",
//	                      "parts":[{"text":"你好"}]}}}
//
// 响应(实测):
//
//	{"jsonrpc":"2.0","id":"...","result":{
//	   "task":{"id":"task-...","contextId":"ctx-...",
//	           "status":{"state":"TASK_STATE_COMPLETED","timestamp":"...",
//	                     "message":{"role":"ROLE_AGENT",
//	                                "parts":[{"text":"...","mediaType":"text/plain"}],
//	                                "messageId":"...","contextId":"ctx-..."}},
//	           "artifacts":[{"artifactId":"...","parts":[{"text":"..."}]}]}}}
//
// ⚠️ v1.0 的 Part 没有 kind 字段 —— 内容分支本身就是判别器
// (text / raw / url / data)。旧 v0.3.0 形状的 kind 只在 legacy 路径兜底。
//
// ## 方法名
//
//	| 版本   | 方法名            |
//	|--------|-------------------|
//	| v1.0   | `SendMessage`     |
//	| v0.3.0 | `message/send`    |
//	| v0.2.x | `tasks/send`(废弃)|
const (
	// MethodSendMessage A2A v1.0 标准方法名(首字母大写)。
	MethodSendMessage = "SendMessage"

	// A2AProtocolVersion 每次请求都要带的头值。
	A2AProtocolVersion = "1.0"
)

// A2AClient 是 A2A v1.0 客户端。
type A2AClient struct {
	c *Client
}

// A2A returns the A2A v1.0 client.
func (c *Client) A2A() *A2AClient {
	return &A2AClient{c: c}
}

// SendMessage 发一条 A2A 消息,返回**原始** JSON-RPC result(map)。
//
// agentURL  —— agent 的完整地址(如 http://10.0.0.1:9901/)
// text      —— 消息正文
// token     —— 可选,agent 开 pairing 认证时的 Bearer token
// contextID —— 可选,多轮对话关联
func (a *A2AClient) SendMessage(
	ctx context.Context,
	agentURL string,
	text string,
	token string,
	contextID string,
) (map[string]any, error) {
	msg := map[string]any{
		"messageId": newUUID(),
		"role":      "user",
		"parts":     []map[string]any{{"text": text}},
	}
	if contextID != "" {
		msg["contextId"] = contextID
	}
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      newUUID(),
		"method":  MethodSendMessage,
		"params":  map[string]any{"message": msg},
	}

	headers := map[string]string{"A2A-Version": A2AProtocolVersion}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}

	var resp map[string]any
	if err := a.c.tp.doWithHeaders(ctx, http.MethodPost, agentURL, body, &resp, headers); err != nil {
		return nil, err
	}
	if e, ok := resp["error"].(map[string]any); ok && len(e) > 0 {
		code := -32603
		if c, ok := e["code"].(float64); ok {
			code = int(c)
		}
		msg := ""
		if m, ok := e["message"].(string); ok {
			msg = m
		}
		return nil, &APIError{StatusCode: code, Body: []byte(msg)}
	}
	if result, ok := resp["result"].(map[string]any); ok {
		return result, nil
	}
	return map[string]any{}, nil
}

// SendText 发一条 A2A 消息,直接返回回复文本。
func (a *A2AClient) SendText(
	ctx context.Context,
	agentURL string,
	text string,
	token string,
	contextID string,
) (string, error) {
	result, err := a.SendMessage(ctx, agentURL, text, token, contextID)
	if err != nil {
		return "", err
	}
	return ExtractA2AText(result), nil
}

// GetAgentCard 拉 agent 卡片(GET {agentURL}/.well-known/agent-card.json)。
func (a *A2AClient) GetAgentCard(ctx context.Context, agentURL string) (map[string]any, error) {
	base := strings.TrimRight(agentURL, "/")
	var card map[string]any
	if err := a.c.tp.do(ctx, http.MethodGet, base+"/.well-known/agent-card.json", nil, &card); err != nil {
		return nil, err
	}
	return card, nil
}

// ExtractA2AText 从 A2A 结构的任意节点里递归提取文本。
//
// 覆盖 v1.0 的三种形状:
//   - task 分支    : {"task":{"status":{"message":{"parts":[...]}},"artifacts":[{"parts":[...]}]}}
//   - message 分支 : {"message":{"parts":[...]}}
//   - legacy v0.3.0: {"kind":"task","artifacts":[{"parts":[{"kind":"text","text":"..."}]}]}
func ExtractA2AText(node any) string {
	var out []string
	seen := map[string]bool{}

	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			// Part:v1.0 用 "text" 字段本身当判别器;legacy 用 "kind":"text"
			if txt, ok := v["text"].(string); ok && txt != "" {
				if !seen[txt] {
					seen[txt] = true
					out = append(out, txt)
				}
			}
			for _, key := range []string{"message", "status", "artifacts", "parts", "result", "task"} {
				if sub, ok := v[key]; ok {
					walk(sub)
				}
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(node)
	return strings.Join(out, "\n")
}

// newUUID 生成一个随机 ID(A2A 的 request id / messageId)。
func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败是极端情况;退化成时间戳拼串仍能保证"每次不同"。
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

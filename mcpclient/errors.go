// Package mcpclient provides an MCP client for WAU.
//
// ⭐ v1.0.0 M11 P6 MCP client (per D87+D88+D89,2026-07-10~11)。
//
// 5 SDK 共享 wire format:JSON-RPC 2.0 over HTTP at POST {baseURL}/mcp
// (跟 WAU-core-kernel internal/protocol/mcp/server.go handleMCP 对齐)。
//
// 本子包 = 7 sync tool wrapper (HealthCheck / ParseAgentCard / SendMessage /
// GetTask / ListTasks / CancelTask / GetExtendedAgentCard) + 2 SSE streaming
// 实装 (StreamMessage / SubscribeToTask,per D89.A.5,2026-07-11) +
// 1 deferred placeholder (CreateTaskPushNotificationConfig,W4 kernel 实装)。
//
// 协议合规:
//   - D60 additive: 0 改老 SDK,独立子包
//   - D13 byte-equal: JSON wire format 5 SDK 一致
//   - D78/D79: Authorization: Bearer <jwt> (W2.0 closure)
//
// 设计原则:
//   - 不引入 testify / gomock,用 net/http + stdlib
//   - HTTPClient 由 caller 注入(同 Options.HTTPClient pattern),便于测试 httptest
//   - 错误统一返 *RPCError,5 spec code + 3 MCP code 跟 kernel mcp.Envelope 一致
package mcpclient

import "fmt"

// RPCError 是 JSON-RPC 2.0 error object 的 Go 表达(per spec + MCP 扩展)。
//
// 跟 kernel mcp.Error 字段 byte-equal:Code / Message / Data。
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error 满足 error interface,Code 跟 Message 拼成单行。
func (e *RPCError) Error() string {
	return fmt.Sprintf("mcp rpc error: code=%d message=%q", e.Code, e.Message)
}

// ────────────────────────────────────────────────────────
// JSON-RPC 2.0 spec error codes(跟 kernel mcp.ErrCode* 一致)
// ────────────────────────────────────────────────────────

const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603

	// MCP-specific(-32000 ~ -32099,per spec 留作 server 端自定义)
	ErrCodeMCPNotInitialized = -32001
	ErrCodeMCPUnknownTool    = -32002
	ErrCodeMCPStreamClosed   = -32003
)

// asRPCError 试图把 error 转成 *RPCError,失败返 nil。
//
// 设计:kernel server 总是返 *RPCError(client 解 Response envelope 拿到);
// HTTP 4xx/5xx 走 transportError 类型,不走这条路径。
func asRPCError(err error) *RPCError {
	if r, ok := err.(*RPCError); ok {
		return r
	}
	return nil
}

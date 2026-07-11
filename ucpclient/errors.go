package ucpclient

import "fmt"

// RPCError 是 JSON-RPC 2.0 error object 的 Go 表达(per spec + UCP 扩展)。
//
// 跟 kernel ucp.Error 字段 byte-equal:Code / Message / Data。
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error 满足 error interface,Code 跟 Message 拼成单行。
func (e *RPCError) Error() string {
	return fmt.Sprintf("ucp rpc error: code=%d message=%q", e.Code, e.Message)
}

// ────────────────────────────────────────────────────────
// JSON-RPC 2.0 spec error codes(跟 kernel ucp.ErrCode* 一致)
// ────────────────────────────────────────────────────────

const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603

	// UCP-specific(-32100 ~ -32199,跟 MCP -32001~32003 错开)
	ErrCodeUCPProductNotFound = -32101
	ErrCodeUCPCartExpired     = -32102
	ErrCodeUCPStripeError     = -32103
	ErrCodeUCPOrderNotFound   = -32104
	ErrCodeUCPPaymentFailed   = -32105
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

// IsNotFound 判断 err 是不是 product / order / cart "not found" 语义错误(UCP spec)。
func IsNotFound(err error) bool {
	if r := asRPCError(err); r != nil {
		return r.Code == ErrCodeUCPProductNotFound ||
			r.Code == ErrCodeUCPOrderNotFound
	}
	return false
}

// IsStripeError 判断 err 是不是 Stripe API 路径错误。
func IsStripeError(err error) bool {
	if r := asRPCError(err); r != nil {
		return r.Code == ErrCodeUCPStripeError ||
			r.Code == ErrCodeUCPPaymentFailed
	}
	return false
}

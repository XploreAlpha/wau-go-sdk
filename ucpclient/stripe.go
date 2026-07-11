package ucpclient

// Stripe helper(SDK 端不直接调 Stripe,所有 Stripe 交互走 kernel 透明)。
//
// 设计原则(per [[process/2026-07-11-W3-UCP-Stripe-Checkout-design]]):
//
//	SDK 0 直接依赖 Stripe SDK — 所有 Stripe API call 都由 kernel
//	internal/protocol/ucp/ucp_stripe.go 转发,SDK 只发常规 HTTP/JSON-RPC。
//	Stripe webhook → kernel POST /v1/ucp/webhooks/stripe →
//	内部走 Stripe SDK + 幂等表 dedup。
//
// 本文件只是 helper 集合(W3 stub 阶段),W5+ 加:
//   - Stripe SDK 错误码 → UCP error code 转换
//   - payment_intent ID 提取
//   - refund flow DTO 转换

// IsStripePath 判断 DTO 是不是跟 Stripe 相关的(create_checkout_session / confirm_payment / cancel_order + refund)。
func IsStripePath(toolName string) bool {
	switch toolName {
	case ToolCreateCheckoutSession, ToolConfirmPayment, ToolCancelOrder:
		return true
	default:
		return false
	}
}

// WaitForPayment — W5+ 实现的占位,SDK 端 polling helper(stub now)。
//
// 设计:由 caller 决定 polling vs webhook;SDK 提供 helper,不强制策略。
// 典型用例:
//
//	for {
//	    conf, err := cli.ConfirmPayment(ctx, sessionID)
//	    if err != nil { return err }
//	    if conf.Status == "succeeded" { ... break }
//	    time.Sleep(2 * time.Second)
//	}
const PaymentStatusSucceeded = "succeeded"
const PaymentStatusFailed = "failed"
const PaymentStatusProcessing = "processing"
const PaymentStatusPending = "pending"

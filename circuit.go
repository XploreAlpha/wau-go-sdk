package wau

import (
	"context"
	"errors"
	"log/slog"

	"github.com/XploreAlpha/circuit"
)

// circuitAdapter 把 wau-circuit 包装成 SDK 友好的 Guard 装饰器。
//
// SDK 端约定:每次 kernel HTTP 请求用 "wau-kernel" 作为 agentID。
// 5 连续失败 → 熔断打开 → 30s 后 HalfOpen → 1 个成功 → Closed。
//
// ADR-0003:SDK 翻译 wau-circuit Go 版直接 import,Python/TS SDK 各翻译 ~150 行。
type circuitAdapter struct {
	cb      *circuit.Breaker
	enabled bool
}

func newCircuitAdapter(cfg CircuitConfig, logger *slog.Logger) *circuitAdapter {
	if !cfg.Enabled {
		return &circuitAdapter{enabled: false}
	}
	cb := circuit.NewBreaker(logger)
	if cfg.FailureThreshold > 0 {
		cb.SetFailureThreshold(int(cfg.FailureThreshold))
	}
	if cfg.OpenTimeout > 0 {
		cb.SetRecoveryTimeout(cfg.OpenTimeout)
	}
	return &circuitAdapter{cb: cb, enabled: true}
}

// Guard 在 op 外面包熔断逻辑。
//
// 行为:
//   - 熔断开 → 短路返 ErrCircuitOpen(不调 op)
//   - 熔断关/半开 → 调 op,然后 RecordSuccess/RecordFailure
func (ca *circuitAdapter) Guard(_ context.Context, op func() error) error {
	if !ca.enabled {
		return op()
	}
	if ca.cb.IsOpen("wau-kernel") {
		return ErrCircuitOpen
	}
	err := op()
	if err != nil {
		if isCircuitFailure(err) {
			ca.cb.RecordFailure("wau-kernel")
		}
		return err
	}
	ca.cb.RecordSuccess("wau-kernel")
	return nil
}

// isCircuitFailure 判断 err 是否应计入熔断失败计数。
//
// 规则:
//   - 5xx 计入
//   - 4xx 不计(业务错,不是服务不可用)
//   - 网络错 / 超时 计入
//   - ErrCircuitOpen 自身不计
func isCircuitFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrCircuitOpen) {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500
	}
	return true
}

// CircuitState 返回当前熔断状态(给 debug / metrics 用)
//
// 包装 wau-circuit.CircuitState 成 SDK 端 string 类型,避免 slog 用户直接依赖
func (ca *circuitAdapter) State() string {
	if !ca.enabled || ca.cb == nil {
		return "closed"
	}
	return ca.cb.GetState("wau-kernel").String()
}

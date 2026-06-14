package wau

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"
)

// retrier 负责指数退避 + 抖动重试。
//
// 策略:backoff(attempt) = min(maxBackoff, initial * 2^attempt) * (1 + uniform(-jitter, +jitter))
// 默认:maxRetries=3 / initial=200ms / max=5s / jitter=0.2
// 只对**幂等**请求自动重试;非幂等 POST 默认不重试(opts override)
type retrier struct {
	cfg     RetryConfig
	sleeper func(time.Duration) // 注入便于测试
	rand    func() float64      // 注入便于测试
}

func newRetrier(cfg RetryConfig) *retrier {
	return &retrier{
		cfg:     cfg,
		sleeper: time.Sleep,
		rand:    rand.Float64,
	}
}

// shouldRetry 判断 err 是否可重试。
//
// 规则:
//   - 5xx 默认可重试(502/503/504 + 其他 5xx 都在 RetryOn)
//   - 4xx 只有 429 触发重试
//   - 网络错(`*url.Error`) 触发重试
//   - 其他错误(业务错、4xx)不重试
func (r *retrier) shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	// 上下文取消立即返
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// 业务错(4xx 非 429)不重试
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		for _, code := range r.cfg.RetryOn {
			if apiErr.StatusCode == code {
				return true
			}
		}
		return false
	}
	// 网络错 / 其它错误 → 重试(因为不知道服务端是否收到)
	return true
}

// backoff 计算第 N 次重试的退避时长(带 jitter)。
func (r *retrier) backoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	delay := r.cfg.InitialBackoff
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay > r.cfg.MaxBackoff {
			delay = r.cfg.MaxBackoff
			break
		}
	}
	// 抖动:[1 - jitter, 1 + jitter]
	j := r.cfg.Jitter
	if j < 0 {
		j = 0
	}
	if j > 1 {
		j = 1
	}
	multiplier := 1.0 + (r.rand()*2-1)*j
	return time.Duration(float64(delay) * multiplier)
}

// Do 在 op 上跑重试循环。
//
// 行为:
//   - op 成功 → 立刻返 nil
//   - op 失败且不可重试 → 立刻返 err
//   - op 失败且可重试 → backoff + 重试,直到 MaxRetries 用完
//   - MaxRetries 用完 → 返 ErrMaxRetries(wraps last err)
func (r *retrier) Do(ctx context.Context, op func() error) error {
	if r.cfg.MaxRetries <= 0 {
		return op()
	}
	var lastErr error
	for attempt := 0; attempt <= r.cfg.MaxRetries; attempt++ {
		if err := op(); err == nil {
			return nil
		} else {
			lastErr = err
			if !r.shouldRetry(err) {
				return err
			}
		}
		// 最后一次不 sleep
		if attempt == r.cfg.MaxRetries {
			break
		}
		delay := r.backoff(attempt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return fmt.Errorf("%w: %v", ErrMaxRetries, lastErr)
}

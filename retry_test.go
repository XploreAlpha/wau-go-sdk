package wau

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetrier_backoff_Exponential(t *testing.T) {
	r := &retrier{
		cfg:     RetryConfig{InitialBackoff: 100 * time.Millisecond, MaxBackoff: 10 * time.Second, Jitter: 0},
		sleeper: func(time.Duration) {},
		rand:    func() float64 { return 0.5 }, // jitter 0 + 0.5 = 1.0 multiplier
	}
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 100 * time.Millisecond},  // 100 * 2^0 = 100
		{1, 200 * time.Millisecond},  // 100 * 2^1 = 200
		{2, 400 * time.Millisecond},  // 100 * 2^2 = 400
		{3, 800 * time.Millisecond},  // 100 * 2^3 = 800
		{4, 1600 * time.Millisecond}, // 100 * 2^4 = 1600
	}
	for _, c := range cases {
		got := r.backoff(c.attempt)
		if got != c.want {
			t.Errorf("backoff(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestRetrier_backoff_MaxCap(t *testing.T) {
	r := &retrier{
		cfg:     RetryConfig{InitialBackoff: 100 * time.Millisecond, MaxBackoff: 500 * time.Millisecond, Jitter: 0},
		sleeper: func(time.Duration) {},
		rand:    func() float64 { return 0.5 },
	}
	// 10 次: 100 * 2^10 = 102400ms,但 cap 500ms
	got := r.backoff(10)
	if got != 500*time.Millisecond {
		t.Errorf("backoff(10) with cap = %v, want 500ms", got)
	}
}

func TestRetrier_backoff_Jitter(t *testing.T) {
	// jitter=0.2 + rand=0.0 → multiplier = 0.8
	r := &retrier{
		cfg:     RetryConfig{InitialBackoff: 100 * time.Millisecond, MaxBackoff: 5 * time.Second, Jitter: 0.2},
		sleeper: func(time.Duration) {},
		rand:    func() float64 { return 0.0 },
	}
	got := r.backoff(0)
	want := time.Duration(float64(100*time.Millisecond) * 0.8)
	if got != want {
		t.Errorf("backoff(0) with jitter 0.2 + rand 0.0 = %v, want %v", got, want)
	}

	// jitter=0.2 + rand=1.0 → multiplier = 1.2
	r.rand = func() float64 { return 1.0 }
	got = r.backoff(0)
	want = time.Duration(float64(100*time.Millisecond) * 1.2)
	if got != want {
		t.Errorf("backoff(0) with jitter 0.2 + rand 1.0 = %v, want %v", got, want)
	}
}

func TestRetrier_shouldRetry_5xx(t *testing.T) {
	r := &retrier{cfg: DefaultRetryConfig()}
	for _, code := range []int{500, 502, 503, 504} {
		err := &APIError{StatusCode: code}
		if !r.shouldRetry(err) {
			t.Errorf("5xx %d 应重试, got false", code)
		}
	}
}

func TestRetrier_shouldRetry_429(t *testing.T) {
	r := &retrier{cfg: DefaultRetryConfig()}
	if !r.shouldRetry(&APIError{StatusCode: 429}) {
		t.Error("429 应重试")
	}
}

func TestRetrier_shouldRetry_4xxNoRetry(t *testing.T) {
	r := &retrier{cfg: DefaultRetryConfig()}
	for _, code := range []int{400, 401, 403, 404, 409} {
		if r.shouldRetry(&APIError{StatusCode: code}) {
			t.Errorf("4xx %d 不应重试, got true", code)
		}
	}
}

func TestRetrier_shouldRetry_NetworkError(t *testing.T) {
	r := &retrier{cfg: DefaultRetryConfig()}
	if !r.shouldRetry(errors.New("dial tcp: connection refused")) {
		t.Error("网络错应重试")
	}
}

func TestRetrier_shouldRetry_ContextCancel(t *testing.T) {
	r := &retrier{cfg: DefaultRetryConfig()}
	if r.shouldRetry(context.Canceled) {
		t.Error("context.Canceled 不应重试")
	}
	if r.shouldRetry(context.DeadlineExceeded) {
		t.Error("context.DeadlineExceeded 不应重试")
	}
}

func TestRetrier_Do_FirstSuccess(t *testing.T) {
	r := &retrier{cfg: DefaultRetryConfig()}
	calls := 0
	err := r.Do(context.Background(), func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestRetrier_Do_4xx_NoRetry(t *testing.T) {
	r := &retrier{cfg: DefaultRetryConfig()}
	calls := 0
	err := r.Do(context.Background(), func() error {
		calls++
		return &APIError{StatusCode: 404, Message: "not found"}
	})
	if err == nil {
		t.Fatal("期望 404 err")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (4xx 不重试)", calls)
	}
}

func TestRetrier_Do_5xx_RetriesAndExhausts(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.MaxRetries = 2
	r := newRetrier(cfg)
	r.sleeper = func(time.Duration) {} // 跳过 sleep
	r.rand = func() float64 { return 0.5 }

	calls := 0
	err := r.Do(context.Background(), func() error {
		calls++
		return &APIError{StatusCode: 503}
	})
	if err == nil {
		t.Fatal("期望重试耗尽 err")
	}
	// 1 + 2 retries = 3 calls
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (1 + 2 retries)", calls)
	}
	if !errors.Is(err, ErrMaxRetries) {
		t.Errorf("err 应 wrap ErrMaxRetries, got %v", err)
	}
}

func TestRetrier_Do_5xx_RecoverAfterRetries(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.MaxRetries = 3
	r := newRetrier(cfg)
	r.sleeper = func(time.Duration) {}
	r.rand = func() float64 { return 0.5 }

	calls := 0
	err := r.Do(context.Background(), func() error {
		calls++
		if calls < 3 {
			return &APIError{StatusCode: 502}
		}
		return nil
	})
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (2 fail + 1 success)", calls)
	}
}

func TestRetrier_Do_NoRetryConfig(t *testing.T) {
	r := &retrier{cfg: RetryConfig{MaxRetries: 0}}
	calls := 0
	_ = r.Do(context.Background(), func() error {
		calls++
		return &APIError{StatusCode: 500}
	})
	if calls != 1 {
		t.Errorf("MaxRetries=0 应只调 1 次, got %d", calls)
	}
}

func TestRetrier_Do_ContextCancel(t *testing.T) {
	cfg := DefaultRetryConfig()
	cfg.MaxRetries = 5
	cfg.InitialBackoff = 50 * time.Millisecond
	r := newRetrier(cfg)
	r.sleeper = func(time.Duration) {}
	r.rand = func() float64 { return 0.5 }

	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	go func() {
		_ = r.Do(ctx, func() error {
			calls.Add(1)
			cancel() // 第 1 次后取消
			return &APIError{StatusCode: 503}
		})
	}()
	// 给 goroutine 跑完
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (cancel 后不再重试)", got)
	}
}

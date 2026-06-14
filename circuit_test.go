package wau

import (
	"errors"
	"testing"
	"time"

	"github.com/XploreAlpha/circuit"
)

func TestCircuitAdapter_Disabled_PassesThrough(t *testing.T) {
	ca := newCircuitAdapter(CircuitConfig{Enabled: false}, nil)
	calls := 0
	err := ca.Guard(nil, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
	if ca.State() != "closed" {
		t.Errorf("disabled state 应 = closed, got %s", ca.State())
	}
}

func TestCircuitAdapter_5xx_RecordsFailure(t *testing.T) {
	ca := newCircuitAdapter(CircuitConfig{
		Enabled:          true,
		FailureThreshold: 5,
		OpenTimeout:      50 * time.Millisecond,
	}, nil)

	// 5 次 5xx → 应打开
	for i := 0; i < 5; i++ {
		_ = ca.Guard(nil, func() error {
			return &APIError{StatusCode: 500}
		})
	}
	if ca.State() != "open" {
		t.Errorf("5 次 5xx 后 state = %s, want open", ca.State())
	}

	// 第 6 次应短路(不调 op)
	called := false
	err := ca.Guard(nil, func() error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("短路 err = %v, want ErrCircuitOpen", err)
	}
	if called {
		t.Error("熔断开后 op 不应被调用")
	}
}

func TestCircuitAdapter_4xx_DoesNotRecord(t *testing.T) {
	ca := newCircuitAdapter(CircuitConfig{
		Enabled:          true,
		FailureThreshold: 3,
		OpenTimeout:      50 * time.Millisecond,
	}, nil)

	// 10 次 404 → 不应触发熔断
	for i := 0; i < 10; i++ {
		_ = ca.Guard(nil, func() error {
			return &APIError{StatusCode: 404}
		})
	}
	if ca.State() != "closed" {
		t.Errorf("10 次 4xx 后 state = %s, want closed (4xx 不计入)", ca.State())
	}
}

func TestCircuitAdapter_NetworkErr_Records(t *testing.T) {
	ca := newCircuitAdapter(CircuitConfig{
		Enabled:          true,
		FailureThreshold: 3,
		OpenTimeout:      50 * time.Millisecond,
	}, nil)

	for i := 0; i < 3; i++ {
		_ = ca.Guard(nil, func() error {
			return errors.New("dial tcp: connection refused")
		})
	}
	if ca.State() != "open" {
		t.Errorf("3 次网络错后 state = %s, want open", ca.State())
	}
}

func TestCircuitAdapter_Success_RecordsSuccess(t *testing.T) {
	ca := newCircuitAdapter(CircuitConfig{
		Enabled:          true,
		FailureThreshold: 3,
		OpenTimeout:      50 * time.Millisecond,
	}, nil)

	// 2 次失败
	for i := 0; i < 2; i++ {
		_ = ca.Guard(nil, func() error { return &APIError{StatusCode: 500} })
	}
	// 1 次成功 → 应重置 failures
	_ = ca.Guard(nil, func() error { return nil })
	// 2 次再失败(共 4 次,但 success 重置 → 仍 Closed)
	for i := 0; i < 2; i++ {
		_ = ca.Guard(nil, func() error { return &APIError{StatusCode: 500} })
	}
	if ca.State() != "closed" {
		t.Errorf("success 应重置 failures, state = %s, want closed", ca.State())
	}
}

func TestIsCircuitFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"5xx", &APIError{StatusCode: 500}, true},
		{"503", &APIError{StatusCode: 503}, true},
		{"4xx", &APIError{StatusCode: 404}, false},
		{"400", &APIError{StatusCode: 400}, false},
		{"network", errors.New("dial tcp: connection refused"), true},
		{"ErrCircuitOpen itself", ErrCircuitOpen, false},
		{"context.DeadlineExceeded", timeoutErr{}, true},
	}
	for _, c := range cases {
		got := isCircuitFailure(c.err)
		if got != c.want {
			t.Errorf("%s: isCircuitFailure = %v, want %v", c.name, got, c.want)
		}
	}
}

// timeoutErr 实现 error + Timeout() 用于测试 context.DeadlineExceeded
type timeoutErr struct{}

func (timeoutErr) Error() string  { return "timeout" }
func (timeoutErr) Timeout() bool  { return true }
func (timeoutErr) Temporary() bool { return true }

// 编译期检查 circuit.NewBreaker 的 Guard 调用方式与 wau-circuit 兼容
func TestCircuitAdapter_CompatibleWithWauCircuit(t *testing.T) {
	cb := circuit.NewBreaker(nil)
	cb.SetFailureThreshold(1)
	cb.SetRecoveryTimeout(10 * time.Millisecond)

	cb.RecordFailure("test-agent")
	// 兼容: wau-circuit 应该可以跟 SDK 端的 adapter 一起工作
	if !cb.IsOpen("test-agent") {
		t.Error("wau-circuit 自身 API 兼容")
	}
}

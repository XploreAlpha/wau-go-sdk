package wau

import (
	"context"
	"testing"
)

// TestClient_NoTracer_DefaultNoop 验证 SDK 在不提供 tracer 时使用 noop 默认。
//
// v0.7.0 W1 验收:OTel 是 optional 集成,默认 noop 应该 0 副作用。
func TestClient_NoTracer_DefaultNoop(t *testing.T) {
	c, err := New("http://localhost:18400")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	if c.tracer == nil {
		t.Error("tracer should default to noop, not nil")
	}
	if _, ok := c.tracer.(noopTracer); !ok {
		t.Errorf("tracer should be noopTracer, got %T", c.tracer)
	}
}

// TestClient_WithTracer 验证 WithTracer 注入自定义 Tracer。
//
// v0.7.0 W1 验收:用户可注入 OTel adapter(或其他),SDK 不会强制 import。
func TestClient_WithTracer(t *testing.T) {
	stub := &stubTracer{}
	c, err := New("http://localhost:18400", WithTracer(stub))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	if c.tracer != stub {
		t.Errorf("tracer should be the stub, got %T", c.tracer)
	}
}

// TestNoopTracer_NoSideEffects 验证 noop tracer 真的什么也不做。
func TestNoopTracer_NoSideEffects(t *testing.T) {
	var t1 noopTracer
	ctx, span := t1.StartSpan(context.Background(), "test")
	if ctx == nil {
		t.Error("StartSpan should return non-nil context")
	}
	if span == nil {
		t.Error("StartSpan should return non-nil span")
	}
	// 这些调用都不应该 panic
	span.SetAttribute("key", "value")
	span.SetAttribute("count", 42)
	span.RecordError(context.Canceled)
	span.End()
}

// stubTracer 是测试用 Tracer,记录所有调用。
type stubTracer struct {
	spans []stubSpan
}

type stubSpan struct {
	opName string
	attrs  map[string]any
	err    error
}

func (s *stubTracer) StartSpan(ctx context.Context, opName string) (context.Context, Span) {
	span := &stubSpan{opName: opName, attrs: make(map[string]any)}
	s.spans = append(s.spans, *span)
	return ctx, span
}

func (s *stubSpan) SetAttribute(key string, value any) { s.attrs[key] = value }
func (s *stubSpan) RecordError(err error)           { s.err = err }
func (s *stubSpan) End()                               {}

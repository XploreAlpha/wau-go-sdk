package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestMockKernel_All5Scenarios 烟测:5 场景黄金 JSON 全部路由正确
func TestMockKernel_All5Scenarios(t *testing.T) {
	srv := NewMockKernel(t)
	defer srv.Close()

	cases := []struct {
		scene  string
		prompt string
		agent  string
		status string
	}{
		{"clinical", "I need clinical decision support for a patient", "Jarvis", "completed"},
		{"france", "What is the capital of France?", "Whis", "completed"},
		{"pain", "Recommend an over-the-counter pain reliever", "Benny", "completed"},
		{"sales", "Show me this quarter's sales analytics", "Whis", "completed"},
		{"rare_disease", "Help me diagnose a rare disease", "Jarvis", "completed"},
	}

	for _, tc := range cases {
		t.Run(tc.scene, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"prompt": tc.prompt, "timeout_ms": 60000})
			resp, err := http.Post(srv.URL+"/registry/tasks/submit", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			raw, _ := io.ReadAll(resp.Body)
			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got["status"] != tc.status {
				t.Errorf("status = %v, want %s", got["status"], tc.status)
			}
			if got["selected_agent"] != tc.agent {
				t.Errorf("selected_agent = %v, want %s", got["selected_agent"], tc.agent)
			}
			if _, ok := got["response"]; !ok {
				t.Error("response 字段缺失")
			}
		})
	}
}

func TestMockKernel_Health(t *testing.T) {
	srv := NewMockKernel(t)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["status"] != "ok" {
		t.Errorf("status = %v, want ok", got["status"])
	}
}

func TestMockKernel_ListAgents(t *testing.T) {
	srv := NewMockKernel(t)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/registry/agents")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	agents, _ := got["agents"].([]any)
	if len(agents) != 3 {
		t.Errorf("agents count = %d, want 3 (Whis/Jarvis/Benny)", len(agents))
	}
}

func TestMockKernel_Simulate_NoActualA2A(t *testing.T) {
	srv := NewMockKernel(t)
	defer srv.Close()
	body, _ := json.Marshal(map[string]any{"prompt": "What is the capital of France?"})
	resp, err := http.Post(srv.URL+"/registry/tasks/simulate", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// simulate 只返 decision,不应有 a2a_call_ms
	if _, hasA2A := got["a2a_call_ms"]; hasA2A {
		t.Error("simulate 响应不应含 a2a_call_ms 字段(没真发)")
	}
	if got["selected_agent"] != "Whis" {
		t.Errorf("selected_agent = %v, want Whis", got["selected_agent"])
	}
}

func TestMockKernel_UnknownPrompt_FallsBackToWhis(t *testing.T) {
	srv := NewMockKernel(t)
	defer srv.Close()
	body, _ := json.Marshal(map[string]any{"prompt": "听不清的问题"})
	resp, err := http.Post(srv.URL+"/registry/tasks/submit", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["selected_agent"] != "Whis" {
		t.Errorf("unknown prompt 应 fallback Whis, got %v", got["selected_agent"])
	}
}

// 防止 sync.Once 在多 test 间串扰,加一个隔离 timer 防误判
func TestMockKernel_StartUpTime(t *testing.T) {
	t0 := time.Now()
	_ = NewMockKernel(t)
	if d := time.Since(t0); d > 500*time.Millisecond {
		t.Errorf("mock kernel 启动太慢: %v", d)
	}
}

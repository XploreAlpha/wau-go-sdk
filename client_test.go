package wau

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 内联 mock kernel(主包内测试用,跨包 import 测试会循环依赖)
func inlineMockKernel(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			_, _ = w.Write([]byte(`{"status":"ok","version":"v0.6.0","uptime":1.0,"redis":"connected"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/kernel/info":
			_, _ = w.Write([]byte(`{"version":"v0.6.0","startTime":"2026-06-14T00:00:00Z","uptime":60,"agentsCount":3,"tasksCount":5}`))
		case r.Method == http.MethodGet && r.URL.Path == "/registry/agents":
			_, _ = w.Write([]byte(`{"agents":[{"name":"Whis","url":"http://whis:18800","skills":["general"],"status":"online","trust":0.85},{"name":"Jarvis","url":"http://jarvis:18800","skills":["clinical"],"status":"online","trust":0.92},{"name":"Benny","url":"http://benny:18800","skills":["pharmaceutical"],"status":"online","trust":0.88}],"total":3,"page":1,"pageSize":10,"totalPages":1}`))
		case strings.HasPrefix(r.URL.Path, "/registry/agents/") && strings.HasSuffix(r.URL.Path, "/status"):
			_, _ = w.Write([]byte(`{"name":"jarvis","status":"online","trust":0.9,"load":{"activeTasks":1,"maxCapacity":10,"cpuUsage":0.2,"memoryUsage":0.3},"circuit":"closed"}`))
		case strings.HasPrefix(r.URL.Path, "/registry/agents/") && strings.HasSuffix(r.URL.Path, "/score"):
			_, _ = w.Write([]byte(`{"name":"jarvis","totalScore":0.88,"trustScore":0.9,"skillMatch":0.85,"healthScore":0.95,"loadScore":0.8}`))
		case r.Method == http.MethodPost && r.URL.Path == "/registry/agents/register":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"name":"test-agent","registered":true}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/registry/agents/"):
			_, _ = w.Write([]byte(`{"name":"test","deregistered":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/registry/agents/heartbeat":
			_, _ = w.Write([]byte(`{"received":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/heartbeat/load":
			_, _ = w.Write([]byte(`{"received":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/registry/tasks/simulate":
			_, _ = w.Write([]byte(`{"selected_agent":"Whis","score":0.55,"decision_time_ms":100,"candidates":[{"name":"Whis","score":0.55,"reason":"general"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/registry/tasks/submit":
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			prompt, _ := req["prompt"].(string)
			if prompt == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"prompt required"}`))
				return
			}
			resp := map[string]any{
				"task_id":        "t1",
				"status":         "completed",
				"selected_agent": "Whis",
				"score":          0.55,
				"decision":       map[string]any{"selected_agent": "Whis", "score": 0.55, "decision_time_ms": 100},
				"a2a_call_ms":    2000,
				"response":       "Echo: " + prompt,
				"source_peer":    "test",
			}
			_ = json.NewEncoder(w).Encode(resp)
		case strings.HasPrefix(r.URL.Path, "/registry/tasks/"):
			_, _ = w.Write([]byte(`{"taskId":"t1","message":"echo","sourcePeer":"test","status":"completed","assignedAgent":"Whis","createdAt":1,"updatedAt":2}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClient_KernelInfo(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	info, err := c.Kernel().Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Version != "v0.6.0" {
		t.Errorf("version = %q", info.Version)
	}
	if info.AgentsCount != 3 {
		t.Errorf("agentsCount = %d", info.AgentsCount)
	}
}

func TestClient_KernelHealth(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	h, err := c.Kernel().Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("status = %q", h.Status)
	}
}

func TestClient_AgentsList(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	got, err := c.Agents().List(context.Background(), PageOptions{PageSize: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got.Agents) != 3 {
		t.Errorf("agents count = %d, want 3", len(got.Agents))
	}
}

func TestClient_AgentsGet(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	got, err := c.Agents().Get(context.Background(), "jarvis")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "online" {
		t.Errorf("status = %q", got.Status)
	}
	if got.Circuit != "closed" {
		t.Errorf("circuit = %q", got.Circuit)
	}
}

func TestClient_AgentsScore(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	s, err := c.Agents().Score(context.Background(), "jarvis")
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if s.TotalScore <= 0 {
		t.Errorf("totalScore = %f", s.TotalScore)
	}
}

func TestClient_AgentsRegister(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	err := c.Agents().Register(context.Background(), AgentRegisterRequest{
		Name:   "test-agent",
		URL:    "http://test:18800",
		Skills: []string{"demo"},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestClient_AgentsDeregister(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	err := c.Agents().Deregister(context.Background(), "test-agent")
	if err != nil {
		t.Fatalf("Deregister: %v", err)
	}
}

func TestClient_AgentsHeartbeat(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	err := c.Agents().Heartbeat(context.Background(), "test-agent")
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
}

func TestClient_AgentsReportLoad(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	err := c.Agents().ReportLoad(context.Background(), "test-agent", AgentLoad{
		ActiveTasks: 1, MaxCapacity: 10, CPUUsage: 0.5, MemoryUsage: 0.6,
	})
	if err != nil {
		t.Fatalf("ReportLoad: %v", err)
	}
}

func TestClient_AgentsIter(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	count := 0
	for a, err := range c.Agents().Iter(context.Background(), PageOptions{PageSize: 10}) {
		if err != nil {
			t.Fatalf("Iter: %v", err)
		}
		if a.Name == "" {
			t.Error("空 name")
		}
		count++
	}
	if count != 3 {
		t.Errorf("iter count = %d, want 3", count)
	}
}

func TestClient_TasksSubmit(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	resp, err := c.Tasks().Submit(context.Background(), SubmitRequest{
		Prompt:    "hello world",
		TimeoutMs: 30000,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if resp.Status != "completed" {
		t.Errorf("status = %q", resp.Status)
	}
	if resp.SelectedAgent != "Whis" {
		t.Errorf("selected = %q", resp.SelectedAgent)
	}
}

func TestClient_TasksSubmit_EmptyPrompt_Errors(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	_, err := c.Tasks().Submit(context.Background(), SubmitRequest{Prompt: ""})
	if err == nil {
		t.Fatal("空 prompt 应返错")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Errorf("err 应是 *APIError, got %T", err)
	} else if apiErr.StatusCode != 400 {
		t.Errorf("status = %d, want 400", apiErr.StatusCode)
	}
}

func TestClient_TasksSimulate(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	dec, err := c.Tasks().Simulate(context.Background(), SubmitRequest{Prompt: "test"})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if dec.SelectedAgent != "Whis" {
		t.Errorf("selected = %q", dec.SelectedAgent)
	}
}

func TestClient_TasksGet(t *testing.T) {
	srv := inlineMockKernel(t)
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	task, err := c.Tasks().Get(context.Background(), "t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if task.TaskID != "t1" {
		t.Errorf("taskId = %q", task.TaskID)
	}
}

func TestClient_BaseURL(t *testing.T) {
	c, _ := New("http://example.com:1234")
	defer c.Close()
	if c.BaseURL() != "http://example.com:1234" {
		t.Errorf("BaseURL = %q", c.BaseURL())
	}
}

func TestClient_Close(t *testing.T) {
	c, _ := New("http://example.com")
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestClient_DefaultBaseURL(t *testing.T) {
	c, _ := New("") // 应 fallback 到 http://localhost:18400
	defer c.Close()
	if c.BaseURL() != "http://localhost:18400" {
		t.Errorf("默认 baseURL = %q", c.BaseURL())
	}
}

func TestClient_CircuitState_DefaultClosed(t *testing.T) {
	c, _ := New("http://example.com")
	defer c.Close()
	if got := c.CircuitState(); got != "closed" {
		t.Errorf("CircuitState = %q, want closed", got)
	}
}

func TestClient_5Scenarios_AllPass(t *testing.T) {
	// 5 场景 prompt 列表,跟 wau-intent e2e_test/test_submit_l4.py 一致
	cases := []struct {
		name     string
		prompt   string
		agent    string
		tokens   []string
	}{
		{"clinical", "I need clinical decision support for a patient", "Jarvis", []string{"临床", "决策"}},
		{"france", "What is the capital of France?", "Whis", []string{"paris"}},
		{"pain", "Recommend an over-the-counter pain reliever", "Benny", []string{"ibuprofen"}},
		{"sales", "Show me this quarter's sales analytics", "Whis", []string{"sales"}},
		{"rare_disease", "Help me diagnose a rare disease", "Jarvis", []string{"罕见病"}},
	}

	// 跨 5 场景的 mock 路由(简单版:每个 prompt 返不同 selected_agent)
	promptToAgent := map[string]string{
		"I need clinical decision support for a patient": "Jarvis",
		"What is the capital of France?":                 "Whis",
		"Recommend an over-the-counter pain reliever":    "Benny",
		"Show me this quarter's sales analytics":         "Whis",
		"Help me diagnose a rare disease":                "Jarvis",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/registry/tasks/submit" {
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			prompt, _ := req["prompt"].(string)
			agent := promptToAgent[prompt]
			if agent == "" {
				agent = "Whis"
			}
			responseText := "Echo: " + prompt
			if agent == "Jarvis" {
				responseText = "【临床决策】根据患者症状,建议生物标志物筛查。" + prompt
			} else if agent == "Benny" {
				responseText = "OTC pain reliever options: ibuprofen, acetaminophen. " + prompt
			} else if strings.Contains(prompt, "France") {
				responseText = "The capital of France is Paris. " + prompt
			} else if strings.Contains(prompt, "sales") {
				responseText = "Sales analytics: revenue up 15% YoY. " + prompt
			} else if agent == "Jarvis" && strings.Contains(prompt, "rare disease") {
				responseText = "【罕见病鉴别诊断】根据患者表型,建议筛查。 " + prompt
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"task_id":        "t1",
				"status":         "completed",
				"selected_agent": agent,
				"score":          0.9,
				"decision":       map[string]any{"selected_agent": agent, "score": 0.9, "decision_time_ms": 100},
				"a2a_call_ms":    2000,
				"response":       responseText,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := c.Tasks().Submit(context.Background(), SubmitRequest{
				Prompt: tc.prompt, TimeoutMs: 30000,
			})
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			if resp.SelectedAgent != tc.agent {
				t.Errorf("selected = %q, want %q", resp.SelectedAgent, tc.agent)
			}
		})
	}
}

// 验证 transport 的 Get/Post/Put/Delete(虽然 4 核心对象不再直接用)
func TestTransport_4Verbs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		_ = bytes.NewReader(body) // touch body
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"ok":true}`))
		case http.MethodPost:
			_, _ = w.Write([]byte(`{"posted":true}`))
		case http.MethodPut:
			_, _ = w.Write([]byte(`{"put":true}`))
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()
	// 通过 4 核心对象触达 Get/Post/Put/Delete:Register(POST)+Deregister(DELETE)+Health(GET) 已覆盖
	// Put 没显式 endpoint,但 transport.Put 自身要在
	tp := c.tp
	_ = tp.Get(context.Background(), "/test-get", nil)
	_ = tp.Post(context.Background(), "/test-post", map[string]string{"k": "v"}, nil)
	_ = tp.Put(context.Background(), "/test-put", map[string]string{"k": "v"}, nil)
	_ = tp.Delete(context.Background(), "/test-delete", nil)
}

// 触发 transport.do 的所有错误路径
func TestTransport_do_NetworkError(t *testing.T) {
	c, _ := New("http://127.0.0.1:1") // 端口不通
	defer c.Close()
	// 用短超时避免测试卡住
	c.opts.HTTPClient = &http.Client{Timeout: 100 * time.Millisecond}
	_, err := c.Kernel().Health(context.Background())
	if err == nil {
		t.Error("期望网络错")
	}
}

func TestTransport_do_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{ not valid json`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()
	_, err := c.Kernel().Health(context.Background())
	if err == nil {
		t.Error("期望 JSON 解码错")
	}
}

func TestTransport_do_4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()
	_, err := c.Kernel().Health(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err 应是 *APIError, got %T", err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("status = %d, want 404", apiErr.StatusCode)
	}
	if !errors.Is(err, ErrNotFound) {
		t.Error("errors.Is(err, ErrNotFound) 应 true")
	}
}

func TestTransport_do_5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`oops`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, WithRetryNo(), WithCircuitDisabled())
	defer c.Close()
	_, err := c.Kernel().Health(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err 应是 *APIError, got %T", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("status = %d, want 500", apiErr.StatusCode)
	}
}

func TestAPIError_ErrorString(t *testing.T) {
	e := &APIError{StatusCode: 404, Code: "not_found", Message: "resource not found", RequestID: "req-1", Body: []byte(`{"x":1}`)}
	got := e.Error()
	if !strings.Contains(got, "404") {
		t.Errorf("Error() 应含 status, got %q", got)
	}
	if !strings.Contains(got, "req-1") {
		t.Errorf("Error() 应含 request_id, got %q", got)
	}
}

func TestAPIError_Is(t *testing.T) {
	a := &APIError{StatusCode: 404, Code: "not_found"}
	b := &APIError{StatusCode: 404, Code: "other"}
	if !a.Is(b) {
		t.Error("同 statusCode 应 Is")
	}
	c := &APIError{StatusCode: 500}
	if a.Is(c) {
		t.Error("不同 statusCode 不应 Is")
	}
}

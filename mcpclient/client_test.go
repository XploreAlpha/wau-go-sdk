package mcpclient

import (
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

// ────────────────────────────────────────────────────────
// Mock MCP server(模拟 WAU-core-kernel handleMCP dispatcher)
// ────────────────────────────────────────────────────────

// mockMCPServer 启动一个 httptest server,模拟 kernel MCP server。
//
// 支持:
//   - POST /mcp 处理 JSON-RPC 2.0 envelope
//   - 7 sync tool + 1 error path + 1 notification(204)
//   - 失败注入(forceHTTPStatus / forceMalformedJSON / forceRPCCode)
type mockMCPServer struct {
	*httptest.Server

	// 记录每个 method 的最近一次调用
	calls []jsonRPCRequest

	// 自定义 tool result map(method name → JSON marshal-able value)
	toolResults map[string]any

	// 注入错误(method name → *RPCError)。如果 method 在这里就返错误,不走 toolResults。
	toolErrors map[string]*RPCError

	// 强制返特定 HTTP status(测试 4xx 路径)
	forceHTTPStatus int

	// 强制返 malformed JSON(测试 unmarshal 错误)
	forceMalformed bool
}

// mockMCPServerOpt 是 mock server 的 functional option。
type mockMCPServerOpt func(*mockMCPServer)

func mockWithResult(method string, result any) mockMCPServerOpt {
	return func(m *mockMCPServer) {
		if m.toolResults == nil {
			m.toolResults = map[string]any{}
		}
		m.toolResults[method] = result
	}
}

func mockWithError(method string, code int, msg string) mockMCPServerOpt {
	return func(m *mockMCPServer) {
		if m.toolErrors == nil {
			m.toolErrors = map[string]*RPCError{}
		}
		m.toolErrors[method] = &RPCError{Code: code, Message: msg}
	}
}

// newMockMCPServer 启动 mock MCP server,返 server + cleanup func。
func newMockMCPServer(t *testing.T, opts ...mockMCPServerOpt) (*mockMCPServer, func()) {
	t.Helper()
	m := &mockMCPServer{
		toolResults: map[string]any{},
		toolErrors:  map[string]*RPCError{},
	}
	for _, opt := range opts {
		opt(m)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", m.handleMCP)

	m.Server = httptest.NewServer(mux)
	return m, m.Server.Close
}

// handleMCP 是 mock server 的 JSON-RPC 2.0 dispatcher。
//
// 镜像 kernel mcp.Server.handleMCP 的行为,但不调 protocol,
// 直接返预设结果。
func (m *mockMCPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if m.forceHTTPStatus != 0 {
		http.Error(w, "forced http error", m.forceHTTPStatus)
		return
	}

	if m.forceMalformed {
		_, _ = w.Write([]byte(`{not valid json`))
		return
	}

	var req jsonRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrCodeParse, Message: err.Error()},
			ID:      0,
		})
		return
	}

	if req.JSONRPC != "2.0" {
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrCodeInvalidRequest, Message: "jsonrpc must be 2.0"},
			ID:      req.ID,
		})
		return
	}

	m.calls = append(m.calls, req)

	// Notification(id == 0 表示省略,per spec):不返 body
	if req.ID == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 提取 tool name
	toolName, _ := req.Params["name"].(string)

	// 优先返 error(测试错误路径)
	if rpcErr, ok := m.toolErrors[toolName]; ok {
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   rpcErr,
			ID:      req.ID,
		})
		return
	}

	// 返 preset result
	result, ok := m.toolResults[toolName]
	if !ok {
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    ErrCodeMCPUnknownTool,
				Message: "unknown tool: " + toolName,
			},
			ID: req.ID,
		})
		return
	}

	// 把 result 包成 envelope;Result 用 RawMessage 由 handler 反序列化
	resultBytes, err := json.Marshal(result)
	if err != nil {
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrCodeInternal, Message: "marshal result: " + err.Error()},
			ID:      req.ID,
		})
		return
	}

	writeRPCResp(w, jsonRPCResponse{
		JSONRPC: "2.0",
		Result:  resultBytes,
		ID:      req.ID,
	})
}

// writeRPCResp 序列化 JSON-RPC 2.0 response + write header。
func writeRPCResp(w http.ResponseWriter, resp jsonRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// ────────────────────────────────────────────────────────
// 7 sync tool happy path
// ────────────────────────────────────────────────────────

// TestClient_HealthCheck 验证 health_check tool → {"status":"ok"}。
func TestClient_HealthCheck(t *testing.T) {
	m, cleanup := newMockMCPServer(t, mockWithResult(ToolHealthCheck, map[string]any{
		"status": "ok",
		"target": map[string]any{"name": "Fox"},
	}))
	defer cleanup()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer c.Close()

	out, err := c.HealthCheck(context.Background(), "Fox")
	if err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
	if out["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", out["status"])
	}
	// 验 server 收到正确的 method + params
	if len(m.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(m.calls))
	}
	if m.calls[0].Method != "tools/call" {
		t.Errorf("expected method=tools/call, got %q", m.calls[0].Method)
	}
	if name, _ := m.calls[0].Params["name"].(string); name != ToolHealthCheck {
		t.Errorf("expected name=%q, got %q", ToolHealthCheck, name)
	}
}

// TestClient_ParseAgentCard 验证 parse_agent_card tool → AgentCard DTO。
func TestClient_ParseAgentCard(t *testing.T) {
	m, cleanup := newMockMCPServer(t, mockWithResult(ToolParseAgentCard, AgentCard{
		Name:    "Fox",
		Version: "1.0.0",
		URL:     "http://fox.example.com",
	}))
	defer cleanup()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// 三种 raw 形式都测:[]byte / string / map[string]any
	t.Run("from []byte", func(t *testing.T) {
		card, err := c.ParseAgentCard(context.Background(), []byte(`{"name":"Fox","version":"1.0.0"}`))
		if err != nil {
			t.Fatal(err)
		}
		if card.Name != "Fox" {
			t.Errorf("expected name=Fox, got %q", card.Name)
		}
	})

	t.Run("from string", func(t *testing.T) {
		card, err := c.ParseAgentCard(context.Background(), `{"name":"Fox"}`)
		if err != nil {
			t.Fatal(err)
		}
		if card.Name != "Fox" {
			t.Errorf("expected name=Fox, got %q", card.Name)
		}
	})

	t.Run("from map", func(t *testing.T) {
		card, err := c.ParseAgentCard(context.Background(), map[string]any{"name": "Fox"})
		if err != nil {
			t.Fatal(err)
		}
		if card.Name != "Fox" {
			t.Errorf("expected name=Fox, got %q", card.Name)
		}
	})
}

// TestClient_SendMessage 验证 send_message tool → Response DTO。
func TestClient_SendMessage(t *testing.T) {
	m, cleanup := newMockMCPServer(t, mockWithResult(ToolSendMessage, Response{
		Kind: "message",
		Message: &Message{
			MessageID: "m-resp",
			Role:      "agent",
			Parts:     []Part{{Kind: "text", Text: "hi from agent"}},
		},
	}))
	defer cleanup()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	resp, err := c.SendMessage(context.Background(), "Fox", &Message{
		MessageID: "m-req",
		Role:      "user",
		Parts:     []Part{{Kind: "text", Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if resp.Message == nil {
		t.Fatal("expected non-nil Message")
	}
	if resp.Message.Role != "agent" {
		t.Errorf("expected role=agent, got %q", resp.Message.Role)
	}
	if resp.Message.Parts[0].Text != "hi from agent" {
		t.Errorf("expected text='hi from agent', got %q", resp.Message.Parts[0].Text)
	}

	// 验 server 收到的 message parts 正确
	last := m.calls[len(m.calls)-1]
	msgRaw, ok := last.Params["message"].(map[string]any)
	if !ok {
		t.Fatalf("expected params.message map, got %T", last.Params["message"])
	}
	parts, ok := msgRaw["parts"].([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("expected 1 part, got %v", msgRaw["parts"])
	}
	part, _ := parts[0].(map[string]any)
	if part["text"] != "hello" {
		t.Errorf("expected text=hello, got %v", part["text"])
	}
}

// TestClient_SendMessage_Validation 验证 caller 端 input validation(避免无谓 network call)。
func TestClient_SendMessage_Validation(t *testing.T) {
	c, err := NewClient("http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.SendMessage(context.Background(), "Fox", nil)
	if err == nil {
		t.Error("expected error for nil message")
	}
	_, err = c.SendMessage(context.Background(), "Fox", &Message{Role: "user"})
	if err == nil {
		t.Error("expected error for empty parts")
	}
}

// TestClient_GetTask 验证 get_task tool → Task DTO。
func TestClient_GetTask(t *testing.T) {
	m, cleanup := newMockMCPServer(t, mockWithResult(ToolGetTask, Task{
		TaskID:    "t-1",
		ContextID: "c-1",
		Status:    TaskStatus{State: "working", Timestamp: "2026-07-11T00:00:00Z"},
	}))
	defer cleanup()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	task, err := c.GetTask(context.Background(), "Fox", "t-1")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if task.TaskID != "t-1" {
		t.Errorf("expected TaskID=t-1, got %q", task.TaskID)
	}
	if task.Status.State != "working" {
		t.Errorf("expected state=working, got %q", task.Status.State)
	}
}

// TestClient_ListTasks 验证 list_tasks tool → ListTasksResult DTO。
func TestClient_ListTasks(t *testing.T) {
	m, cleanup := newMockMCPServer(t, mockWithResult(ToolListTasks, ListTasksResult{
		Tasks: []Task{
			{TaskID: "t-1", Status: TaskStatus{State: "working"}},
			{TaskID: "t-2", Status: TaskStatus{State: "completed"}},
		},
		Total: 2,
	}))
	defer cleanup()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// 测试带 filter
	result, err := c.ListTasks(context.Background(), "Fox", &TaskFilter{
		State: "working",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if result.Total != 2 {
		t.Errorf("expected total=2, got %d", result.Total)
	}
	if len(result.Tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(result.Tasks))
	}

	// 验 filter 序列化正确
	last := m.calls[len(m.calls)-1]
	filterRaw, ok := last.Params["filter"].(map[string]any)
	if !ok {
		t.Fatalf("expected params.filter map, got %T", last.Params["filter"])
	}
	if filterRaw["state"] != "working" {
		t.Errorf("expected state=working, got %v", filterRaw["state"])
	}
}

// TestClient_ListTasks_NilFilter 验证 nil filter 也走得通。
func TestClient_ListTasks_NilFilter(t *testing.T) {
	m, cleanup := newMockMCPServer(t, mockWithResult(ToolListTasks, ListTasksResult{
		Tasks: nil,
		Total: 0,
	}))
	defer cleanup()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.ListTasks(context.Background(), "Fox", nil)
	if err != nil {
		t.Fatalf("ListTasks with nil filter failed: %v", err)
	}

	// 验 params.filter 不存在
	last := m.calls[len(m.calls)-1]
	if _, exists := last.Params["filter"]; exists {
		t.Errorf("expected no filter key, got %v", last.Params["filter"])
	}
}

// TestClient_CancelTask 验证 cancel_task tool → canceled Task DTO。
func TestClient_CancelTask(t *testing.T) {
	m, cleanup := newMockMCPServer(t, mockWithResult(ToolCancelTask, Task{
		TaskID: "t-cancel",
		Status: TaskStatus{State: "canceled"},
	}))
	defer cleanup()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	task, err := c.CancelTask(context.Background(), "Fox", "t-cancel")
	if err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}
	if task.Status.State != "canceled" {
		t.Errorf("expected state=canceled, got %q", task.Status.State)
	}
}

// TestClient_GetExtendedAgentCard 验证 get_extended_agent_card tool → ExtendedAgentCard DTO。
func TestClient_GetExtendedAgentCard(t *testing.T) {
	m, cleanup := newMockMCPServer(t, mockWithResult(ToolGetExtendedAgentCard, ExtendedAgentCard{
		AgentCard: AgentCard{
			Name:    "Fox",
			Version: "2.0.0",
		},
		Capabilities: map[string]any{
			"streaming": true,
			"max_tasks": float64(100),
		},
	}))
	defer cleanup()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	card, err := c.GetExtendedAgentCard(context.Background(), "Fox")
	if err != nil {
		t.Fatalf("GetExtendedAgentCard failed: %v", err)
	}
	if card.Name != "Fox" {
		t.Errorf("expected name=Fox, got %q", card.Name)
	}
	if card.Capabilities["streaming"] != true {
		t.Errorf("expected capabilities.streaming=true, got %v", card.Capabilities["streaming"])
	}
}

// ────────────────────────────────────────────────────────
// 1 envelope error path
// ────────────────────────────────────────────────────────

// TestClient_RPCError 验证 server 返 *RPCError → client 也返 *RPCError,code/message 完整。
func TestClient_RPCError(t *testing.T) {
	m, cleanup := newMockMCPServer(t, mockWithError(ToolSendMessage, ErrCodeInvalidParams, "missing 'target'"))
	defer cleanup()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.SendMessage(context.Background(), "Fox", &Message{
		Role:  "user",
		Parts: []Part{{Kind: "text", Text: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %T (%v)", err, err)
	}
	if rpcErr.Code != ErrCodeInvalidParams {
		t.Errorf("expected code=%d, got %d", ErrCodeInvalidParams, rpcErr.Code)
	}
	if rpcErr.Message != "missing 'target'" {
		t.Errorf("expected message='missing target', got %q", rpcErr.Message)
	}
}

// TestClient_UnknownTool 验证 server 返 -32002 MCPUnknownTool → client 透传。
func TestClient_UnknownTool(t *testing.T) {
	m, cleanup := newMockMCPServer(t, mockWithError(ToolHealthCheck, ErrCodeMCPUnknownTool, "unknown tool"))
	defer cleanup()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.HealthCheck(context.Background(), "Fox")
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %T", err)
	}
	if rpcErr.Code != ErrCodeMCPUnknownTool {
		t.Errorf("expected code=%d, got %d", ErrCodeMCPUnknownTool, rpcErr.Code)
	}
}

// TestClient_HTTPError 验证 5xx → *RPCError(走 fallback,code 转负数)。
func TestClient_HTTPError(t *testing.T) {
	m := &mockMCPServer{forceHTTPStatus: http.StatusInternalServerError}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", m.handleMCP)
	m.Server = httptest.NewServer(mux)
	defer m.Server.Close()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.HealthCheck(context.Background(), "Fox")
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %T", err)
	}
	if rpcErr.Code != -500 {
		t.Errorf("expected code=-500, got %d", rpcErr.Code)
	}
}

// TestClient_MalformedJSON 验证 server 返 malformed JSON → client 返 unmarshal error。
func TestClient_MalformedJSON(t *testing.T) {
	m := &mockMCPServer{forceMalformed: true}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", m.handleMCP)
	m.Server = httptest.NewServer(mux)
	defer m.Server.Close()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.HealthCheck(context.Background(), "Fox")
	if err == nil {
		t.Fatal("expected error for malformed response")
	}
	// 验错误信息里提到 unmarshal
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("expected unmarshal error, got %v", err)
	}
}

// ────────────────────────────────────────────────────────
// Client options
// ────────────────────────────────────────────────────────

// TestClient_WithBearerToken 验证 Authorization header 正确注入。
func TestClient_WithBearerToken(t *testing.T) {
	mux := http.NewServeMux()
	gotAuth := ""
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		// 返健康检查结果
		resultBytes, _ := json.Marshal(map[string]any{"status": "ok"})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			Result:  resultBytes,
			ID:      1,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewClient(srv.URL, WithBearerToken("jwt-abc-123"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.HealthCheck(context.Background(), "Fox")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer jwt-abc-123" {
		t.Errorf("expected Authorization='Bearer jwt-abc-123', got %q", gotAuth)
	}
}

// TestClient_WithUserAgent 验证 User-Agent override 生效。
func TestClient_WithUserAgent(t *testing.T) {
	mux := http.NewServeMux()
	gotUA := ""
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		resultBytes, _ := json.Marshal(map[string]any{"status": "ok"})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			Result:  resultBytes,
			ID:      1,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewClient(srv.URL, WithUserAgent("custom-agent/1.0"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.HealthCheck(context.Background(), "Fox")
	if err != nil {
		t.Fatal(err)
	}
	if gotUA != "custom-agent/1.0" {
		t.Errorf("expected User-Agent=custom-agent/1.0, got %q", gotUA)
	}
}

// TestClient_WithEndpoint 验证自定义 endpoint path(用于测试)。
func TestClient_WithEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	hit := false
	mux.HandleFunc("/custom/mcp", func(w http.ResponseWriter, r *http.Request) {
		hit = true
		resultBytes, _ := json.Marshal(map[string]any{"status": "ok"})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			Result:  resultBytes,
			ID:      1,
		})
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("default /mcp should not be hit when WithEndpoint used")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewClient(srv.URL, WithEndpoint("/custom/mcp"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.HealthCheck(context.Background(), "Fox")
	if err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Error("expected /custom/mcp to be hit")
	}
}

// TestNewClient_BaseURLValidation 验证空 baseURL → error。
func TestNewClient_BaseURLValidation(t *testing.T) {
	_, err := NewClient("")
	if err == nil {
		t.Error("expected error for empty baseURL")
	}
}

// TestNewClient_TrailingSlash 验证 baseURL 末尾 slash 被去掉(避免双 slash)。
func TestNewClient_TrailingSlash(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		resultBytes, _ := json.Marshal(map[string]any{"status": "ok"})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			Result:  resultBytes,
			ID:      1,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewClient(srv.URL + "/") // trailing slash
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// 不报错 = 成功(没拼接成 "//mcp")
	_, err = c.HealthCheck(context.Background(), "Fox")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// ────────────────────────────────────────────────────────
// Request ID 自增
// ────────────────────────────────────────────────────────

// TestClient_RequestIDIncrement 验证多次调用 ID 自增。
func TestClient_RequestIDIncrement(t *testing.T) {
	m, cleanup := newMockMCPServer(t, mockWithResult(ToolHealthCheck, map[string]any{"status": "ok"}))
	defer cleanup()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	for i := 1; i <= 3; i++ {
		_, err = c.HealthCheck(context.Background(), "Fox")
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(m.calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(m.calls))
	}
	for i, call := range m.calls {
		expected := int64(i + 1) // 1, 2, 3
		if call.ID != expected {
			t.Errorf("call %d: expected ID=%d, got %d", i, expected, call.ID)
		}
	}
}

// ────────────────────────────────────────────────────────
// Context cancellation
// ────────────────────────────────────────────────────────

// TestClient_ContextCancel 验证 ctx 取消 → client 返 error(不 hang)。
func TestClient_ContextCancel(t *testing.T) {
	// mock server 在 handler 里 block,模拟慢响应
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewClient(srv.URL, WithHTTPClient(&http.Client{Timeout: 1 * time.Second}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err = c.HealthCheck(ctx, "Fox")
	if err == nil {
		t.Fatal("expected error after ctx cancel")
	}
}

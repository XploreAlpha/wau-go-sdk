package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ────────────────────────────────────────────────────────
// Mock SSE MCP server(D89.A.5)
//
// 模拟 kernel MCP server:
//  - POST /mcp {tools/call, name=stream_message|subscribe_to_task} → 返 {stream_id, endpoint}
//  - GET  <endpoint>?stream_id=<uuid>  → text/event-stream
// ────────────────────────────────────────────────────────

// sseScriptFrame 是 mock server 收到 GET SSE 时按顺序发射的 SSE frame。
type sseScriptFrame struct {
	// eventType 是 SSE `event:` 字段(open/message/artifact/task_status/
	// task_complete/close/error),空 = 不发 event 行。
	eventType string

	// data 是 SSE `data:` 行(原样发送,不做 marshal)。
	data string

	// delay 控制 frame 发送前的 sleep(模拟真实 server 节奏)。
	delay time.Duration
}

// mockSSEServer 是 mock kernel MCP server(SSE + JSON-RPC 双端)。
type mockSSEServer struct {
	*httptest.Server

	// streamIDCounter 每次 POST stream_message/subscribe_to_task 时 +1。
	streamIDCounter atomic.Int64

	// script 是 GET SSE 时按顺序发射的 frame 列表。
	script []sseScriptFrame

	// forceHTTPStatus POST /mcp 时强制返特定 status(测试 4xx 路径)。
	forceHTTPStatus int

	// forceHTTPStatusGET GET /mcp/sse 时强制返特定 status。
	forceHTTPStatusGET int

	// gotPOSTParams 记录最近一次 POST /mcp 的 params(测试验证)。
	gotPOSTParams map[string]any

	// gotGetHeaders 记录 GET /mcp/sse 时的 request header。
	gotGetHeaders http.Header

	// gotGetPath 记录 GET 请求的 path + query。
	gotGetPath string

	// mu 保护脚本和 headers。
	mu sync.Mutex
}

// newMockSSEServer 启动 mock server,SSE script 由 caller 提供。
func newMockSSEServer(t *testing.T, script []sseScriptFrame) (*mockSSEServer, func()) {
	t.Helper()
	m := &mockSSEServer{script: script}

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", m.handleMCP)
	mux.HandleFunc("/mcp/sse", m.handleSSE)
	mux.HandleFunc("/mcp/sse/", m.handleSSE) // 兼容 ?stream_id=...
	m.Server = httptest.NewServer(mux)
	return m, m.Server.Close
}

// handleMCP 处理 POST /mcp。
func (m *mockSSEServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	defer r.Body.Close()
	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrCodeParse, Message: err.Error()},
			ID:      0,
		})
		return
	}

	if m.forceHTTPStatus != 0 {
		http.Error(w, "forced http error", m.forceHTTPStatus)
		return
	}

	toolName, _ := req.Params["name"].(string)

	m.mu.Lock()
	m.gotPOSTParams = req.Params
	m.mu.Unlock()

	switch toolName {
	case ToolStreamMessage, ToolSubscribeToTask:
		// 返 {stream_id, endpoint}
		streamID := fmt.Sprintf("stream-%d", m.streamIDCounter.Add(1))
		result := streamOpenResult{
			StreamID: streamID,
			Endpoint: "/mcp/sse",
		}
		resultBytes, _ := json.Marshal(result)
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Result:  resultBytes,
			ID:      req.ID,
		})
	default:
		// 其它 tool 走通用未知错误
		writeRPCResp(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrCodeMCPUnknownTool, Message: "unknown tool: " + toolName},
			ID:      req.ID,
		})
	}
}

// handleSSE 处理 GET /mcp/sse,按 m.script 顺序写 SSE frame。
func (m *mockSSEServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.gotGetHeaders = r.Header.Clone()
	m.gotGetPath = r.URL.RequestURI()
	m.mu.Unlock()

	if m.forceHTTPStatusGET != 0 {
		http.Error(w, "forced http error", m.forceHTTPStatusGET)
		return
	}

	// 提取请求的 stream_id(用于替换 script 里 "{{stream_id}}" 占位符)
	reqStreamID := r.URL.Query().Get("stream_id")

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	for _, frame := range m.script {
		if frame.delay > 0 {
			time.Sleep(frame.delay)
		}
		// 检查 ctx(client 可能 cancel 了)
		select {
		case <-r.Context().Done():
			return
		default:
		}
		data := frame.data
		// 占位符替换:{{stream_id}} → 实际 stream_id
		if reqStreamID != "" {
			data = strings.ReplaceAll(data, "{{stream_id}}", reqStreamID)
		}
		if frame.eventType != "" {
			_, _ = fmt.Fprintf(w, "event: %s\n", frame.eventType)
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
	// 持续 hold connection,直到 client 断开。
	<-r.Context().Done()
}

// writeSSERaw 写自定义 SSE 行序列(给复杂测试用)。
func writeSSERaw(w http.ResponseWriter, flusher http.Flusher, lines ...string) {
	for _, line := range lines {
		_, _ = fmt.Fprint(w, line+"\n")
	}
	_, _ = fmt.Fprint(w, "\n")
	flusher.Flush()
}

// ────────────────────────────────────────────────────────
// Happy path tests
// ────────────────────────────────────────────────────────

// TestStreamMessage_OpenFrame 验证 POST 返 stream_id,GET SSE 返 open frame。
func TestStreamMessage_OpenFrame(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"stream-1","timestamp":"2026-07-11T10:00:00Z"}`},
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, err := NewClient(m.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role:  "user",
		Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatalf("StreamMessage failed: %v", err)
	}
	defer handle.Cancel()

	if handle.StreamID() != "stream-1" {
		t.Errorf("expected stream_id=stream-1, got %q", handle.StreamID())
	}

	// 收 open frame
	ev := <-handle.Events()
	if ev.Type != "open" {
		t.Errorf("expected type=open, got %q", ev.Type)
	}
	if ev.StreamID != "stream-1" {
		t.Errorf("expected ev.StreamID=stream-1, got %q", ev.StreamID)
	}
	if ev.Timestamp != "2026-07-11T10:00:00Z" {
		t.Errorf("expected timestamp, got %q", ev.Timestamp)
	}
}

// TestStreamMessage_MessageFrame 验证收 3 个 message event。
func TestStreamMessage_MessageFrame(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"stream-1","timestamp":"2026-07-11T10:00:00Z"}`},
		{eventType: "message", data: `{"role":"agent","parts":[{"kind":"text","text":"hello"}]}`},
		{eventType: "message", data: `{"role":"agent","parts":[{"kind":"text","text":"world"}]}`},
		{eventType: "message", data: `{"role":"agent","parts":[{"kind":"text","text":"!"}]}`},
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	var messages []StreamEvent
	// 等 close + 读剩余
	for ev := range handle.Events() {
		if ev.Type == "message" {
			messages = append(messages, ev)
		}
		if ev.Type == "close" {
			break
		}
	}
	if len(messages) != 3 {
		t.Errorf("expected 3 message events, got %d", len(messages))
	}
}

// TestStreamMessage_ArtifactFrame 验证收 artifact event。
func TestStreamMessage_ArtifactFrame(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"stream-1"}`},
		{eventType: "artifact", data: `{"artifact_id":"a1","name":"file.txt","parts":[{"kind":"text","text":"content"}]}`},
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "generate file"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	var foundArtifact bool
	for ev := range handle.Events() {
		if ev.Type == "artifact" {
			foundArtifact = true
			var art Artifact
			if err := json.Unmarshal(ev.Data, &art); err != nil {
				t.Errorf("artifact unmarshal failed: %v", err)
			}
			if art.ArtifactID != "a1" {
				t.Errorf("expected artifact_id=a1, got %q", art.ArtifactID)
			}
		}
		if ev.Type == "close" {
			break
		}
	}
	if !foundArtifact {
		t.Error("expected artifact event")
	}
}

// TestStreamMessage_TaskCompleteFrame 验证收 task_complete + close。
func TestStreamMessage_TaskCompleteFrame(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"stream-1"}`},
		{eventType: "task_status", data: `{"state":"working"}`},
		{eventType: "task_complete", data: `{"task_id":"t-1","status":{"state":"completed"}}`},
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "do work"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	var sawTaskComplete bool
	for ev := range handle.Events() {
		if ev.Type == "task_complete" {
			sawTaskComplete = true
		}
		if ev.Type == "close" {
			break
		}
	}
	if !sawTaskComplete {
		t.Error("expected task_complete event")
	}
}

// TestStreamMessage_ErrorFrame 验证收 error frame → handle.Err() 返 *RPCError。
func TestStreamMessage_ErrorFrame(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"stream-1"}`},
		{eventType: "error", data: `{"code":-32003,"message":"stream closed by upstream"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	// 等 done channel
	<-handle.Done()

	if handle.Err() == nil {
		t.Fatal("expected Err() to be set")
	}
	var rpcErr *RPCError
	if !errors.As(handle.Err(), &rpcErr) {
		t.Fatalf("expected *RPCError, got %T", handle.Err())
	}
	if rpcErr.Code != ErrCodeMCPStreamClosed {
		t.Errorf("expected code=%d, got %d", ErrCodeMCPStreamClosed, rpcErr.Code)
	}
}

// ────────────────────────────────────────────────────────
// Error / failure tests
// ────────────────────────────────────────────────────────

// TestStreamMessage_401 验证 HTTP 401 → *RPCError。
func TestStreamMessage_401(t *testing.T) {
	m, cleanup := newMockSSEServer(t, nil)
	defer cleanup()
	m.forceHTTPStatusGET = http.StatusUnauthorized

	c, _ := NewClient(m.URL, WithBearerToken("bad-token"))
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatalf("StreamMessage POST failed: %v", err)
	}
	defer handle.Cancel()

	<-handle.Done()

	if handle.Err() == nil {
		t.Fatal("expected error from 401")
	}
	var rpcErr *RPCError
	if !errors.As(handle.Err(), &rpcErr) {
		t.Fatalf("expected *RPCError, got %T", handle.Err())
	}
	if rpcErr.Code != -401 {
		t.Errorf("expected code=-401, got %d", rpcErr.Code)
	}
}

// TestStreamMessage_404 验证 GET 返 404 → ErrCodeMCPStreamClosed。
func TestStreamMessage_404(t *testing.T) {
	m, cleanup := newMockSSEServer(t, nil)
	defer cleanup()
	m.forceHTTPStatusGET = http.StatusNotFound

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer handle.Cancel()

	<-handle.Done()

	var rpcErr *RPCError
	if !errors.As(handle.Err(), &rpcErr) {
		t.Fatalf("expected *RPCError, got %T", handle.Err())
	}
	if rpcErr.Code != ErrCodeMCPStreamClosed {
		t.Errorf("expected code=%d, got %d", ErrCodeMCPStreamClosed, rpcErr.Code)
	}
}

// TestStreamMessage_NilMessage 验证 nil msg → error(无网络调用)。
func TestStreamMessage_NilMessage(t *testing.T) {
	c, _ := NewClient("http://localhost")
	defer c.Close()

	_, err := c.StreamMessage(context.Background(), "Fox", nil, nil)
	if err == nil {
		t.Fatal("expected error for nil message")
	}
}

// TestStreamMessage_EmptyParts 验证空 parts → error。
func TestStreamMessage_EmptyParts(t *testing.T) {
	c, _ := NewClient("http://localhost")
	defer c.Close()

	_, err := c.StreamMessage(context.Background(), "Fox", &Message{Role: "user"}, nil)
	if err == nil {
		t.Fatal("expected error for empty parts")
	}
}

// TestStreamMessage_PostFailure 验证 POST /mcp 返 500 → StreamMessage 返 error。
func TestStreamMessage_PostFailure(t *testing.T) {
	m, cleanup := newMockSSEServer(t, nil)
	defer cleanup()
	m.forceHTTPStatus = http.StatusInternalServerError

	c, _ := NewClient(m.URL)
	defer c.Close()

	_, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err == nil {
		t.Fatal("expected error from POST 500")
	}
}

// TestStreamMessage_GetFailure 验证 GET /mcp/sse 返 500 → handle.Err()。
func TestStreamMessage_GetFailure(t *testing.T) {
	m, cleanup := newMockSSEServer(t, nil)
	defer cleanup()
	m.forceHTTPStatusGET = http.StatusInternalServerError

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatalf("POST should succeed: %v", err)
	}
	defer handle.Cancel()

	<-handle.Done()

	if handle.Err() == nil {
		t.Fatal("expected error from GET 500")
	}
}

// ────────────────────────────────────────────────────────
// Context / cancellation
// ────────────────────────────────────────────────────────

// TestStreamMessage_ContextCancel 验证 ctx cancel → close stream + close channel。
func TestStreamMessage_ContextCancel(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"stream-1"}`},
		// 长时间不结束
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	handle, err := c.StreamMessage(ctx, "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 等 open frame
	ev := <-handle.Events()
	if ev.Type != "open" {
		t.Fatalf("expected open, got %s", ev.Type)
	}

	// cancel ctx
	cancel()
	// Wait for reader goroutine to exit
	select {
	case <-handle.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expected Done() to close after ctx cancel")
	}
	// 调用 Cancel 关闭 channel
	if err := handle.Cancel(); err != nil {
		t.Errorf("Cancel error: %v", err)
	}
	// channel should be closed now
	_, ok := <-handle.Events()
	if ok {
		t.Error("expected Events() channel to be closed")
	}
}

// ────────────────────────────────────────────────────────
// SubscribeToTask tests
// ────────────────────────────────────────────────────────

// TestSubscribeToTask_HappyPath 验证基本订阅 + 2 task_status event。
func TestSubscribeToTask_HappyPath(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"stream-1"}`},
		{eventType: "task_status", data: `{"state":"working","timestamp":"2026-07-11T10:00:01Z"}`},
		{eventType: "task_status", data: `{"state":"completed","timestamp":"2026-07-11T10:00:02Z"}`},
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.SubscribeToTask(context.Background(), "Fox", "task-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	var statuses []StreamEvent
	for ev := range handle.Events() {
		if ev.Type == "task_status" {
			statuses = append(statuses, ev)
		}
		if ev.Type == "close" {
			break
		}
	}
	if len(statuses) != 2 {
		t.Errorf("expected 2 task_status events, got %d", len(statuses))
	}
}

// TestSubscribeToTask_TaskIDEmpty 验证空 taskID → error。
func TestSubscribeToTask_TaskIDEmpty(t *testing.T) {
	c, _ := NewClient("http://localhost")
	defer c.Close()

	_, err := c.SubscribeToTask(context.Background(), "Fox", "", nil)
	if err == nil {
		t.Fatal("expected error for empty task_id")
	}
}

// TestSubscribeToTask_ErrorFrame 验证 error frame → RPCError。
func TestSubscribeToTask_ErrorFrame(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"stream-1"}`},
		{eventType: "error", data: `{"code":-32603,"message":"internal error"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.SubscribeToTask(context.Background(), "Fox", "task-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	<-handle.Done()
	var rpcErr *RPCError
	if !errors.As(handle.Err(), &rpcErr) {
		t.Fatalf("expected *RPCError, got %T", handle.Err())
	}
	if rpcErr.Code != -32603 {
		t.Errorf("expected code=-32603, got %d", rpcErr.Code)
	}
}

// TestSubscribeToTask_StreamOptions 验证 opts 传到 POST params。
func TestSubscribeToTask_StreamOptions(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	opts := &StreamOptions{IncludeHistory: true, IncludeArtifacts: true}
	handle, err := c.SubscribeToTask(context.Background(), "Fox", "task-1", opts)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	for range handle.Events() {
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	gotArgs, ok := m.gotPOSTParams["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("expected arguments map, got %T", m.gotPOSTParams["arguments"])
	}
	gotOpts, ok := gotArgs["stream_options"].(map[string]any)
	if !ok {
		t.Fatalf("expected stream_options map, got %T", gotArgs["stream_options"])
	}
	if gotOpts["include_history"] != true {
		t.Errorf("expected include_history=true, got %v", gotOpts["include_history"])
	}
	if gotOpts["include_artifacts"] != true {
		t.Errorf("expected include_artifacts=true, got %v", gotOpts["include_artifacts"])
	}
}

// ────────────────────────────────────────────────────────
// StreamOptions JSON encoding tests
// ────────────────────────────────────────────────────────

// TestStreamOptions_IncludeHistory 验证 IncludeHistory JSON 正确。
func TestStreamOptions_IncludeHistory(t *testing.T) {
	opts := StreamOptions{IncludeHistory: true}
	b, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["include_history"] != true {
		t.Errorf("expected include_history=true, got %v", got)
	}
	if _, exists := got["include_artifacts"]; exists {
		t.Errorf("expected include_artifacts omitted (false), got present")
	}
}

// TestStreamOptions_IncludeArtifacts 验证 IncludeArtifacts JSON 正确。
func TestStreamOptions_IncludeArtifacts(t *testing.T) {
	opts := StreamOptions{IncludeArtifacts: true}
	b, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["include_artifacts"] != true {
		t.Errorf("expected include_artifacts=true, got %v", got)
	}
	if _, exists := got["include_history"]; exists {
		t.Errorf("expected include_history omitted (false), got present")
	}
}

// TestStreamOptions_Nil 验证 nil opts 不带 stream_options 字段。
func TestStreamOptions_Nil(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	for range handle.Events() {
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	gotArgs, ok := m.gotPOSTParams["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("expected arguments map, got %T", m.gotPOSTParams["arguments"])
	}
	if _, exists := gotArgs["stream_options"]; exists {
		t.Errorf("expected no stream_options key, got %v", gotArgs["stream_options"])
	}
}

// ────────────────────────────────────────────────────────
// StreamHandle tests
// ────────────────────────────────────────────────────────

// TestStreamHandle_CancelTwice 验证 Cancel 幂等。
func TestStreamHandle_CancelTwice(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"stream-1"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-handle.Events() // open

	if err := handle.Cancel(); err != nil {
		t.Errorf("first Cancel failed: %v", err)
	}
	if err := handle.Cancel(); err != nil {
		t.Errorf("second Cancel failed (expected idempotent nil): %v", err)
	}
}

// TestStreamHandle_EventsClosedAfterCancel 验证 Cancel 后 channel 关闭。
func TestStreamHandle_EventsClosedAfterCancel(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"stream-1"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-handle.Events() // open

	if err := handle.Cancel(); err != nil {
		t.Fatal(err)
	}
	// channel should be closed
	select {
	case _, ok := <-handle.Events():
		if ok {
			t.Error("expected channel closed after Cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for channel close")
	}
}

// ────────────────────────────────────────────────────────
// Auth / advanced tests
// ────────────────────────────────────────────────────────

// TestStreamMessage_BearerTokenSent 验证 bearer token 通过 GET /mcp/sse header 发送。
func TestStreamMessage_BearerTokenSent(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"stream-1"}`},
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL, WithBearerToken("jwt-abc"))
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	for range handle.Events() {
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if got := m.gotGetHeaders.Get("Authorization"); got != "Bearer jwt-abc" {
		t.Errorf("expected Authorization='Bearer jwt-abc', got %q", got)
	}
	if got := m.gotGetHeaders.Get("Accept"); got != "text/event-stream" {
		t.Errorf("expected Accept='text/event-stream', got %q", got)
	}
}

// TestStreamMessage_BearerTokenRefresh 验证 caller 拿到 401 后可以重新尝试。
//
// 此测试不直接验证 SDK 内部 token 刷新逻辑(SDK 不自动刷新),
// 而是模拟 caller 拿到 RPCError 后用新 token 再开 stream。
func TestStreamMessage_BearerTokenRefresh(t *testing.T) {
	// 第一次调用:401
	m1, cleanup1 := newMockSSEServer(t, nil)
	defer cleanup1()
	m1.forceHTTPStatusGET = http.StatusUnauthorized

	c1, _ := NewClient(m1.URL, WithBearerToken("old-token"))
	defer c1.Close()

	h1, err := c1.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-h1.Done()
	var rpcErr *RPCError
	if !errors.As(h1.Err(), &rpcErr) {
		t.Fatalf("expected *RPCError on 401, got %T", h1.Err())
	}
	h1.Cancel()

	// 第二次调用:新 token + 新 server,成功
	m2, cleanup2 := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"s2"}`},
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup2()

	c2, _ := NewClient(m2.URL, WithBearerToken("new-token"))
	defer c2.Close()

	h2, err := c2.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Cancel()

	for ev := range h2.Events() {
		if ev.Type == "close" {
			break
		}
	}
}

// TestStreamMessage_StreamIDMismatch 验证 server 返 stream_id 不匹配 → 报错。
func TestStreamMessage_StreamIDMismatch(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		// POST 会返 stream_id=stream-1,但 GET 返的 open frame 用了不同 stream_id
		{eventType: "open", data: `{"stream_id":"stream-WRONG","timestamp":"2026-07-11T10:00:00Z"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	<-handle.Done()
	if handle.Err() == nil {
		t.Fatal("expected error from stream_id mismatch")
	}
	if !strings.Contains(handle.Err().Error(), "stream_id mismatch") {
		t.Errorf("expected mismatch error, got %v", handle.Err())
	}
}

// TestStreamMessage_InvalidJSON 验证 SSE data: 行 malformed → error。
func TestStreamMessage_InvalidJSON(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `not-valid-json`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	<-handle.Done()
	if handle.Err() == nil {
		t.Fatal("expected error from malformed data")
	}
	if !strings.Contains(handle.Err().Error(), "malformed") {
		t.Errorf("expected malformed error, got %v", handle.Err())
	}
}

// TestStreamMessage_MultipleEvents 验证收 10 个 event 顺序正确。
func TestStreamMessage_MultipleEvents(t *testing.T) {
	var frames []sseScriptFrame
	for i := 0; i < 10; i++ {
		frames = append(frames, sseScriptFrame{
			eventType: "message",
			data:      fmt.Sprintf(`{"role":"agent","parts":[{"kind":"text","text":"msg-%d"}]}`, i),
		})
	}
	frames = append(frames, sseScriptFrame{eventType: "close", data: `{"reason":"done"}`})

	m, cleanup := newMockSSEServer(t, frames)
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	count := 0
	for ev := range handle.Events() {
		if ev.Type == "message" {
			count++
			// 验内容
			var msg struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			}
			if err := json.Unmarshal(ev.Data, &msg); err != nil {
				t.Fatalf("event %d unmarshal: %v", count, err)
			}
			expected := fmt.Sprintf("msg-%d", count-1)
			if msg.Parts[0].Text != expected {
				t.Errorf("event %d: expected text=%q, got %q", count, expected, msg.Parts[0].Text)
			}
		}
		if ev.Type == "close" {
			break
		}
	}
	if count != 10 {
		t.Errorf("expected 10 message events, got %d", count)
	}
}

// TestStreamMessage_ConcurrentStreams 验证同时开 3 个 stream。
func TestStreamMessage_ConcurrentStreams(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"{{stream_id}}"}`},
		{eventType: "message", data: `{"text":"a"}`},
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	const nStreams = 3
	handles := make([]*StreamHandle, nStreams)
	for i := 0; i < nStreams; i++ {
		h, err := c.StreamMessage(context.Background(), "Fox", &Message{
			Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
		}, nil)
		if err != nil {
			t.Fatalf("StreamMessage %d failed: %v", i, err)
		}
		handles[i] = h
	}

	// 并发读
	var wg sync.WaitGroup
	for i, h := range handles {
		wg.Add(1)
		go func(idx int, handle *StreamHandle) {
			defer wg.Done()
			var got int
			for ev := range handle.Events() {
				if ev.Type == "message" {
					got++
				}
				if ev.Type == "close" {
					break
				}
			}
			if got != 1 {
				t.Errorf("stream %d: expected 1 message, got %d", idx, got)
			}
		}(i, h)
	}

	// 全部 wait 完再 cancel
	doneAll := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneAll)
	}()

	select {
	case <-doneAll:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for concurrent streams")
	}

	for _, h := range handles {
		_ = h.Cancel()
	}
}

// TestStreamMessage_StreamOptionsCustom 验证 custom opts 传到 POST params。
func TestStreamMessage_StreamOptionsCustom(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	opts := &StreamOptions{IncludeHistory: false, IncludeArtifacts: true}
	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	for range handle.Events() {
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	gotArgs, _ := m.gotPOSTParams["arguments"].(map[string]any)
	gotOpts, _ := gotArgs["stream_options"].(map[string]any)
	if gotOpts["include_artifacts"] != true {
		t.Errorf("expected include_artifacts=true, got %v", gotOpts)
	}
	if _, exists := gotOpts["include_history"]; exists {
		t.Errorf("expected include_history omitted, got present")
	}
}

// TestStreamMessage_LongRunning 验证收 50 个 event,中途 cancel。
func TestStreamMessage_LongRunning(t *testing.T) {
	var frames []sseScriptFrame
	for i := 0; i < 50; i++ {
		frames = append(frames, sseScriptFrame{
			eventType: "message",
			data:      fmt.Sprintf(`{"role":"agent","parts":[{"kind":"text","text":"msg-%d"}]}`, i),
		})
	}
	m, cleanup := newMockSSEServer(t, frames)
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 读到 25 个时 cancel
	count := 0
	for ev := range handle.Events() {
		if ev.Type == "message" {
			count++
			if count == 25 {
				if err := handle.Cancel(); err != nil {
					t.Errorf("Cancel error: %v", err)
				}
				break
			}
		}
	}
	if count != 25 {
		t.Errorf("expected to read 25 events before cancel, got %d", count)
	}
}

// TestStreamHandle_TimestampParsing 验证 ISO 8601 timestamp 解析正确。
func TestStreamHandle_TimestampParsing(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"stream-1","timestamp":"2026-07-11T10:30:45.123Z"}`},
		{eventType: "task_status", data: `{"state":"working","timestamp":"2026-07-11T10:30:46.500Z"}`},
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	for ev := range handle.Events() {
		if ev.Type == "open" {
			if ev.Timestamp != "2026-07-11T10:30:45.123Z" {
				t.Errorf("expected open timestamp, got %q", ev.Timestamp)
			}
		}
		if ev.Type == "task_status" {
			if ev.Timestamp != "2026-07-11T10:30:46.500Z" {
				t.Errorf("expected task_status timestamp, got %q", ev.Timestamp)
			}
		}
		if ev.Type == "close" {
			break
		}
	}
}

// TestStreamMessage_SSECommentLines 验证 ":" 注释行被忽略。
func TestStreamMessage_SSECommentLines(t *testing.T) {
	// 用 mockSSEServer 发带注释行的 SSE
	m, cleanup2 := newMockSSEServerWithRaw(t, func(w http.ResponseWriter, flusher http.Flusher, streamID string) {
		data := strings.ReplaceAll(`{"stream_id":"{{stream_id}}","timestamp":"2026-07-11T10:00:00Z"}`, "{{stream_id}}", streamID)
		_, _ = fmt.Fprint(w, ": this is a comment\n")
		_, _ = fmt.Fprint(w, ":another comment\n")
		_, _ = fmt.Fprint(w, "event: open\n")
		_, _ = fmt.Fprint(w, "data: "+data+"\n")
		_, _ = fmt.Fprint(w, "\n")
		_, _ = fmt.Fprint(w, ": trailing comment\n")
		_, _ = fmt.Fprint(w, "event: close\n")
		_, _ = fmt.Fprint(w, "data: {\"reason\":\"done\"}\n\n")
		flusher.Flush()
	})
	defer cleanup2()

	c2, _ := NewClient(m.URL)
	defer c2.Close()

	handle, err := c2.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	var gotOpen, gotClose bool
	for ev := range handle.Events() {
		if ev.Type == "open" {
			gotOpen = true
		}
		if ev.Type == "close" {
			gotClose = true
			break
		}
	}
	if !gotOpen {
		t.Error("expected open event despite comment lines")
	}
	if !gotClose {
		t.Error("expected close event despite comment lines")
	}
}

// newMockSSEServerWithRaw 启动一个 mock server,GET /mcp/sse 走 caller 提供的 raw writer。
//
// writeFn 收到的 SSE body 字符串中 "{{stream_id}}" 会被替换为实际请求的 stream_id。
func newMockSSEServerWithRaw(t *testing.T, writeFn func(w http.ResponseWriter, flusher http.Flusher, streamID string)) (*mockSSEServer, func()) {
	t.Helper()
	m := &mockSSEServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", m.handleMCP)
	mux.HandleFunc("/mcp/sse", func(w http.ResponseWriter, r *http.Request) {
		streamID := r.URL.Query().Get("stream_id")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		writeFn(w, flusher, streamID)
		<-r.Context().Done()
	})
	m.Server = httptest.NewServer(mux)
	return m, m.Server.Close
}

// ────────────────────────────────────────────────────────
// StreamHandle.Done / Err tests
// ────────────────────────────────────────────────────────

// TestStreamHandle_DoneOnClose 验证 normal close → Done() closed + Err() nil。
func TestStreamHandle_DoneOnClose(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "open", data: `{"stream_id":"stream-1"}`},
		{eventType: "close", data: `{"reason":"client_done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	<-handle.Done()
	if err := handle.Err(); err != nil {
		t.Errorf("expected nil Err on normal close, got %v", err)
	}
}

// TestStreamMessage_TargetAgentRef 验证 target 作为 *AgentRef 也走得通。
func TestStreamMessage_TargetAgentRef(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), &AgentRef{Name: "Fox", Universe: "cyber"}, &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	for range handle.Events() {
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	gotArgs, _ := m.gotPOSTParams["arguments"].(map[string]any)
	gotTarget, ok := gotArgs["target"].(map[string]any)
	if !ok {
		t.Fatalf("expected target map, got %T", gotArgs["target"])
	}
	if gotTarget["name"] != "Fox" {
		t.Errorf("expected name=Fox, got %v", gotTarget["name"])
	}
	if gotTarget["universe"] != "cyber" {
		t.Errorf("expected universe=cyber, got %v", gotTarget["universe"])
	}
}

// TestStreamMessage_NoStreamIDInResponse 验证 POST 返 result 缺 stream_id → error。
func TestStreamMessage_NoStreamIDInResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		// 返一个不带 stream_id 的 result
		resultBytes, _ := json.Marshal(map[string]string{"foo": "bar"})
		writeRPCResp(w, jsonRPCResponse{JSONRPC: "2.0", Result: resultBytes, ID: 1})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := NewClient(srv.URL)
	defer c.Close()

	_, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err == nil {
		t.Fatal("expected error from missing stream_id")
	}
	if !strings.Contains(err.Error(), "stream_id") {
		t.Errorf("expected error mentioning stream_id, got %v", err)
	}
}

// TestStreamMessage_UserAgentSent 验证 User-Agent header 在 GET /mcp/sse 时也设置。
func TestStreamMessage_UserAgentSent(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL, WithUserAgent("custom-agent/2.0"))
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	for range handle.Events() {
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if got := m.gotGetHeaders.Get("User-Agent"); got != "custom-agent/2.0" {
		t.Errorf("expected User-Agent=custom-agent/2.0, got %q", got)
	}
}

// TestStreamMessage_StreamIDQueryParam 验证 GET /mcp/sse URL 包含 stream_id。
func TestStreamMessage_StreamIDQueryParam(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	for range handle.Events() {
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if !strings.Contains(m.gotGetPath, "stream_id=") {
		t.Errorf("expected stream_id query param in GET path, got %q", m.gotGetPath)
	}
	if !strings.Contains(m.gotGetPath, "stream-1") {
		t.Errorf("expected stream_id=stream-1, got %q", m.gotGetPath)
	}
}

// TestStreamMessage_AcceptHeaderSent 验证 Accept header 是 text/event-stream。
func TestStreamMessage_AcceptHeaderSent(t *testing.T) {
	m, cleanup := newMockSSEServer(t, []sseScriptFrame{
		{eventType: "close", data: `{"reason":"done"}`},
	})
	defer cleanup()

	c, _ := NewClient(m.URL)
	defer c.Close()

	handle, err := c.StreamMessage(context.Background(), "Fox", &Message{
		Role: "user", Parts: []Part{{Kind: "text", Text: "hi"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Cancel()

	for range handle.Events() {
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if got := m.gotGetHeaders.Get("Accept"); got != "text/event-stream" {
		t.Errorf("expected Accept=text/event-stream, got %q", got)
	}
	if got := m.gotGetHeaders.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("expected Cache-Control=no-cache, got %q", got)
	}
}

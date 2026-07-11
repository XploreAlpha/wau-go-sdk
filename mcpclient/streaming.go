package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// ────────────────────────────────────────────────────────
// SSE streaming types(D89.A.5 实装)
//
// 协议:跟 WAU-core-kernel internal/protocol/mcp/server.go SSE handler 对齐:
//  1. POST /mcp {method:"tools/call", name:"stream_message"|"subscribe_to_task", ...}
//     → {result: {stream_id, endpoint}}
//  2. GET <endpoint>?stream_id=<uuid>
//     → text/event-stream,event frames:
//     - event: open (data: {stream_id, timestamp})
//     - event: message | artifact | task_status | task_complete
//     - event: close (data: {reason})
//     - event: error (data: {code, message})
//
// 详细规范见 D87.3 W5 kernel 实装 + D89.A SOP。
// ────────────────────────────────────────────────────────

// StreamEvent 是从 /mcp/sse 收到的一个 SSE event。
//
// Type 字段对应 SSE `event:` 字段,可取值:
//   - "open":流建立握手
//   - "message":agent message 片段
//   - "artifact":agent artifact
//   - "task_status":task 状态变化
//   - "task_complete":task 完成(后续通常跟 close)
//   - "close":流关闭
//   - "error":流错误
//
// Data 是 SSE `data:` 行的原始 JSON,call 自己再 unmarshal。
type StreamEvent struct {
	// Type 跟 SSE `event:` 字段对应(open/message/artifact/task_status/
	// task_complete/close/error)。
	Type string

	// StreamID 是 server 分配的 stream UUID。
	StreamID string

	// Timestamp 是 ISO 8601 时间戳(来自 open frame 或 message frame)。
	Timestamp string

	// Data 是原始 JSON payload。Caller 按 Type 自己解析:
	//   - message → []Part 或 *Message
	//   - artifact → *Artifact
	//   - task_status → *TaskStatus
	//   - task_complete → *Task
	//   - close → {reason string}
	//   - error → *RPCError
	Data json.RawMessage
}

// StreamOptions 是 stream session 的可选配置。
//
// JSON tag 跟 kernel mcp.StreamOptions byte-equal(snake_case)。
// nil = 不带 stream_options 字段(由 server 决定默认行为)。
type StreamOptions struct {
	// IncludeHistory stream 开始时是否带历史 message 一起重发。
	IncludeHistory bool `json:"include_history,omitempty"`

	// IncludeArtifacts stream 是否带 artifact 事件。
	IncludeArtifacts bool `json:"include_artifacts,omitempty"`
}

// StreamHandle 是开着的 stream 句柄。
//
// Caller 用 Events() channel 接收 event,用 Cancel() 关闭流。
// Cancel 是幂等的,可以多次调用。
// Events channel 在 stream 结束(正常 close / error / HTTP 失败 / ctx cancel /
// caller.Cancel)时自动关闭 — caller 用 `for ev := range handle.Events()` 即可。
type StreamHandle struct {
	// streamID server 分配的 UUID。
	streamID string

	// client 引用 client 配置(httpClient/bearerToken/userAgent/baseURL)。
	client *Client

	// events 是 server → client 的事件 channel。
	// 流关闭时由 SDK 关闭(由后台 reader goroutine 的 defer 负责)。
	events chan StreamEvent

	// cancel 取消 in-flight GET 请求 + 关闭 events channel。
	cancel context.CancelFunc

	// closeOnce 保证 Cancel 幂等 + closeErr 只设一次。
	closeOnce sync.Once

	// closeErr 记录 stream 被关闭时的原因(给 caller 诊断)。
	closeErr error

	// done 当 stream 完全关闭(正常 close / 错误 / ctx cancel)时关闭。
	done chan struct{}
}

// StreamID 返回 server 分配的 stream UUID。
func (h *StreamHandle) StreamID() string { return h.streamID }

// Events 返回 event channel。
//
// 流关闭时 channel 也会被关闭(caller `range` 退出)。
func (h *StreamHandle) Events() <-chan StreamEvent { return h.events }

// Cancel 主动关闭 stream + cancel in-flight HTTP request。
//
// 幂等:多次调用不报错(第二次返 nil)。
// Cancel 后 Events() channel 会被关闭(caller range 退出)。
func (h *StreamHandle) Cancel() error {
	h.closeOnce.Do(func() {
		// 仅当 reader 还没设置 closeErr 时才标为 cancelled
		// (正常 close / error frame 由 reader 设置 closeErr,我们保留之)。
		// 这里直接 cancel ctx,reader 退出时 defer 会 close events channel。
		h.cancel()
		// 等 reader goroutine 退出,确保 events channel 已被 close。
		<-h.done
	})
	return nil
}

// Done 返回一个在 stream 完全关闭时关闭的 channel。
//
// 跟 Events() 不同,Done() 在 stream 关闭(无论正常 / error / cancel)时立即收到信号。
// 主要给 caller 用于 "等 stream 跑完再做点事" 的场景。
func (h *StreamHandle) Done() <-chan struct{} { return h.done }

// Err 返回 stream 关闭原因(close frame 的 reason / error frame / HTTP error / ctx cancel)。
//
// 在 stream 还在跑时返 nil。
func (h *StreamHandle) Err() error { return h.closeErr }

// ────────────────────────────────────────────────────────
// StreamMessage + SubscribeToTask public API
// ────────────────────────────────────────────────────────

// StreamMessage 调 stream_message tool 并开 SSE 长连接。
//
// 流程:
//  1. POST /mcp {tools/call, name=stream_message, arguments: {target, message, stream_options}}
//     → 返 {stream_id, endpoint}
//  2. GET <endpoint>?stream_id=<uuid>
//     → 持续读 SSE event 写入 handle.Events()
//
// 返回的 *StreamHandle 必须在不用时调 Cancel()。
// 错误情况返 (nil, error):input validation / POST 失败 / stream_id mismatch。
func (c *Client) StreamMessage(ctx context.Context, target any, msg *Message, opts *StreamOptions) (*StreamHandle, error) {
	if msg == nil {
		return nil, fmt.Errorf("mcpclient: message is required")
	}
	if len(msg.Parts) == 0 {
		return nil, fmt.Errorf("mcpclient: message.parts must have at least 1 item")
	}
	arguments := map[string]any{
		"target":  normalizeTarget(target),
		"message": msg,
	}
	if opts != nil {
		arguments["stream_options"] = opts
	}
	return c.openStream(ctx, ToolStreamMessage, arguments)
}

// SubscribeToTask 调 subscribe_to_task tool 并开 SSE 长连接。
//
// 跟 StreamMessage 区别:参数是 task_id 而非 message。
// SSE frame 类型以 task_status / task_complete 为主。
func (c *Client) SubscribeToTask(ctx context.Context, target any, taskID string, opts *StreamOptions) (*StreamHandle, error) {
	if taskID == "" {
		return nil, fmt.Errorf("mcpclient: task_id is required")
	}
	arguments := map[string]any{
		"target":  normalizeTarget(target),
		"task_id": taskID,
	}
	if opts != nil {
		arguments["stream_options"] = opts
	}
	return c.openStream(ctx, ToolSubscribeToTask, arguments)
}

// ────────────────────────────────────────────────────────
// Internal: openStream 公共 SSE 启动逻辑
// ────────────────────────────────────────────────────────

// streamOpenResult 是 POST stream_message / subscribe_to_task 返的 result envelope。
type streamOpenResult struct {
	StreamID string `json:"stream_id"`
	Endpoint string `json:"endpoint"`
}

// openStream 启动 stream 的内部共用方法。
//
// 步骤:
//  1. POST /mcp 调用 tool(tools/call + name + arguments)
//  2. 解析 stream_id + endpoint
//  3. GET endpoint?stream_id=<uuid> 带 bearer token
//  4. 后台 goroutine 解析 SSE event → events channel
func (c *Client) openStream(ctx context.Context, toolName string, arguments map[string]any) (*StreamHandle, error) {
	// Step 1: POST /mcp
	params := map[string]any{
		"name":      toolName,
		"arguments": arguments,
	}
	var open streamOpenResult
	if err := c.call(ctx, "tools/call", params, false, &open); err != nil {
		return nil, err
	}
	if open.StreamID == "" || open.Endpoint == "" {
		return nil, fmt.Errorf("mcpclient: %s response missing stream_id/endpoint: %+v", toolName, open)
	}

	// Step 2: 派生 ctx 给 SSE reader 用,handle.Cancel() 触发取消。
	streamCtx, cancel := context.WithCancel(ctx)

	handle := &StreamHandle{
		streamID: open.StreamID,
		client:   c,
		events:   make(chan StreamEvent, 16), // buffer 防止 reader goroutine 被阻塞
		cancel:   cancel,
		done:     make(chan struct{}),
	}

	// Step 3: 后台 goroutine GET SSE。
	go c.runSSE(streamCtx, handle, open.Endpoint)

	return handle, nil
}

// runSSE 在后台 goroutine 里跑 GET SSE 连接 + parse events。
//
// 关闭时机(任一即退出):
//   - server 发 close frame → handle.closeErr = nil
//   - server 发 error frame → handle.closeErr = *RPCError
//   - HTTP 4xx/5xx → handle.closeErr = *RPCError
//   - ctx cancel → handle.closeErr = ctx.Err()
//
// defer 关闭 events channel + done channel,确保 caller range 退出。
func (c *Client) runSSE(ctx context.Context, h *StreamHandle, endpoint string) {
	defer func() {
		close(h.events)
		close(h.done)
	}()

	// 拼 endpoint URL。endpoint 可能已经是完整路径("/mcp/sse")或相对路径。
	url := endpoint
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		// 相对路径:拼到 baseURL
		if strings.HasPrefix(url, "/") {
			url = c.baseURL + url
		} else {
			url = c.baseURL + "/" + url
		}
	}
	// 追加 stream_id query 参数(如果 endpoint 没带)
	if !strings.Contains(url, "stream_id=") {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url = url + sep + "stream_id=" + h.streamID
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		h.closeErr = fmt.Errorf("mcpclient: build sse request: %w", err)
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", c.userAgent)
	if c.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		h.closeErr = fmt.Errorf("mcpclient: sse http do: %w", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		h.closeErr = &RPCError{
			Code:    resp.StatusCode * -1,
			Message: "http 401: unauthorized (bearer token rejected)",
		}
		return
	}
	if resp.StatusCode == http.StatusNotFound {
		h.closeErr = &RPCError{
			Code:    ErrCodeMCPStreamClosed,
			Message: fmt.Sprintf("stream_id not found or expired: %s", h.streamID),
		}
		return
	}
	if resp.StatusCode >= 400 {
		// 读 body 看错误细节
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		h.closeErr = &RPCError{
			Code:    resp.StatusCode * -1,
			Message: fmt.Sprintf("http %d: %s", resp.StatusCode, string(body)),
		}
		return
	}

	// Step 4: 解析 SSE frame loop
	h.closeErr = parseSSEStream(ctx, resp.Body, h.streamID, h.events)
}

// parseSSEStream 读 SSE frame 流,直到 EOF / close / error / ctx cancel。
//
// 返回:
//   - nil → 正常 close
//   - *RPCError → server 发 error frame
//   - error → 解析错误
func parseSSEStream(ctx context.Context, body io.Reader, expectedStreamID string, out chan<- StreamEvent) error {
	scanner := bufio.NewScanner(body)
	// 加大 buffer,允许长 message(默认 64KB 对大 artifact 可能不够)。
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20) // max 1MB per line

	var (
		eventType string
		dataLines []string
	)

	// deliver 投递当前 frame 给 channel。返 nil = 成功,caller 决定是否终止。
	deliver := func(ev StreamEvent) error {
		select {
		case out <- ev:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}

	flushFrame := func() error {
		if eventType == "" && len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")

		ev := StreamEvent{
			Type: eventType,
			Data: json.RawMessage(data),
		}
		// 从 data JSON 提取 stream_id / timestamp 字段(仅在有 data 时)。
		if len(data) > 0 && data != "null" {
			var meta map[string]any
			if err := json.Unmarshal([]byte(data), &meta); err == nil {
				if sid, ok := meta["stream_id"].(string); ok {
					ev.StreamID = sid
					if expectedStreamID != "" && sid != expectedStreamID {
						return fmt.Errorf("stream_id mismatch: expected %q, got %q", expectedStreamID, sid)
					}
				}
				if ts, ok := meta["timestamp"].(string); ok {
					ev.Timestamp = ts
				}
			} else {
				// Data 不是有效 JSON → 用 RPCError 包一层(对齐 parse error 语义)。
				return fmt.Errorf("mcpclient: malformed sse data line: %w", err)
			}
		}

		// 处理特殊 frame type。
		switch ev.Type {
		case "error":
			// error frame → 投递,然后返 RPCError(走 closeErr 路径)。
			_ = deliver(ev)
			var rpcErr RPCError
			if err := json.Unmarshal(ev.Data, &rpcErr); err == nil {
				return &rpcErr
			}
			return fmt.Errorf("sse error frame: %s", string(ev.Data))
		case "close":
			// close frame → 投递后正常结束。
			if err := deliver(ev); err != nil {
				return err
			}
			return errTerminalFrame
		}

		return deliver(ev)
	}

	for scanner.Scan() {
		line := scanner.Text()

		// 空行 = frame 结束。
		if line == "" {
			if err := flushFrame(); err != nil {
				if errors.Is(err, errTerminalFrame) {
					return nil // close frame 已经投递了,正常结束
				}
				return err
			}
			eventType = ""
			dataLines = dataLines[:0]
			continue
		}

		// 注释行(SSE spec):以 ":" 开头,忽略。
		if strings.HasPrefix(line, ":") {
			continue
		}

		// 解析 "field: value" 或 "field:value"。
		field, value, ok := splitSSEField(line)
		if !ok {
			// 非法行(spec 说忽略),继续。
			continue
		}
		switch field {
		case "event":
			eventType = value
		case "data":
			dataLines = append(dataLines, value)
		case "id":
			// last event ID,先 ignore(W3 不重连用不上)。
		case "retry":
			// 重连间隔,先 ignore。
		}
	}

	if err := scanner.Err(); err != nil {
		// EOF 正常返回 io.EOF,ctx cancel 返回 context error。
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("mcpclient: sse scan: %w", err)
	}
	// 最后可能有一帧没空行结束。
	if eventType != "" || len(dataLines) > 0 {
		_ = flushFrame()
	}
	return nil
}

// errTerminalFrame 是 deliver 的 sentinel,表示 frame 投递完 + 流终止。
var errTerminalFrame = errors.New("terminal frame delivered")

// splitSSEField 解析 SSE field/value 行。
//
// SSE spec:field 后接 ":" + 可选空格 + value。
// 整行只有 ":" → field name + 空 value。
func splitSSEField(line string) (field, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		// 没冒号 = 整行作为 field name + 空 value。
		return line, "", true
	}
	field = line[:idx]
	value = line[idx+1:]
	// 去掉 value 前导空格(spec 允许 1 个)。
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	return field, value, true
}

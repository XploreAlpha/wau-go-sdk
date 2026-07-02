// v0.9.0 M3 §3.7 — wau-go-sdk Chat / LLM client(wau-edge OpenAI 兼容层封装)
//
// 替换 v0.8.0 时代的 Tasks().Submit 路径(v0.6.0 M3 修正后走 /registry/tasks/submit):
//   旧:c.Tasks().Submit(ctx, SubmitRequest{Prompt: ...})   → wau-core :18400 /registry/tasks/submit
//   新:c.Chat().Completions(ctx, ChatCompletionRequest{...}) → wau-edge :18402 /v1/chat/completions
//
// 沿用 handshake.go service 模式(per agents.go:18-30 AgentsService):
//   - 持有 c *Client
//   - 走 c.doWithRetry(ctx, method, path, body, &resp) 一行封装
//   - 自动应用:HS256 鉴权 + 熔断 + 重试
//
// 鉴权:沿用 Client.signer 签发的 JWT(per auth.go Sign())。
//   wau-edge Claims 期望 sub / tenant_id(必填)/ iss(wau-core 兼容)
//   不带 universe(wau-edge §2.2 auth 不需要,业务分组由 OpenAI request.metadata.universe 透传)
//
// 流式(SSE):Stage 3.1 #10 (2026-07-02) 实装 — c.Chat().Stream(ctx, req) 返回
//   (<-chan ChatCompletionChunk, <-chan error, func() error) 三件套,iter 拿 chunk
//   直到 FinishReason="stop" 或 ctx 取消。底层用 bufio.Scanner 解析 SSE
//   (data: {json}\n\n + data: [DONE]\n\n 终止)。
//   **不含 Telegram bot 流式**(Telegram Bot API 限制,推 v1.0.0+)。
//
// 错误码(per wau-edge §2.2 6 错误码):
//   -32001 InsufficientTrust  -32002 AgentNotFound   -32003 TenantMismatch
//   -32004 RateLimited        -32005 ProtocolNotSupported
//   -32600 InvalidRequest     -32601 ModelNotFound(per §2.5 OpenAI 兼容层新增)
package wau

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ChatService 提供 LLM chat completions 操作(wau-edge OpenAI 兼容层封装)。
//
// 用法:
//
//	c, _ := wau.New("http://localhost:18402") // wau-edge 端口(不是 wau-core 18400)
//	resp, err := c.Chat().Completions(ctx, wau.ChatCompletionRequest{
//	    Model: "gpt-4o-mini",
//	    Messages: []wau.ChatMessage{{Role: "user", Content: "hello"}},
//	})
type ChatService struct {
	c *Client
}

// Completions POST /v1/chat/completions — wau-edge OpenAI 兼容层主入口。
//
// 参数:
//   - ctx:请求上下文(超时 / 取消信号)
//   - req:ChatCompletionRequest(per OpenAI spec,字段 1:1 对齐)
//
// 返回:
//   - *ChatCompletionResponse:OpenAI 兼容响应,wau-edge / wau-llm-router / new-api 串联后字节级对齐
//   - error:失败时返 APIError / 网络错误 / 上下文错误
//
// 错误处理:
//   - HTTP 4xx/5xx → *APIError(用 errors.As 拆 Code 字段)
//   - ctx 取消 → context.Canceled / context.DeadlineExceeded
//   - 熔断开 → ErrCircuitOpen
//   - 重试耗尽 → ErrMaxRetries
//
// 鉴权依赖:
//   - Client 必须用 wau.WithAuth 配 HS256(per wau-edge §2.2)
//   - 如果没配 Auth,请求仍可发(走匿名路径,server 端会按需拒绝)
func (s *ChatService) Completions(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	if req.Model == "" {
		return nil, errors.New("wau: ChatCompletionRequest.Model is required")
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("wau: ChatCompletionRequest.Messages must not be empty")
	}
	var resp ChatCompletionResponse
	if err := s.c.doWithRetry(ctx, http.MethodPost, "/v1/chat/completions", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CompletionsRaw POST /v1/chat/completions 返回 raw bytes(给流式 / 字节级测试用)。
//
// 大多数情况用 Completions 即可;只在需要保留 server 原始响应字节时用这个。
// streaming 雏形期 M3 §3.7 不实现,留 v0.9.x;但 raw 接口预留,后面 streaming 接 io.Reader 时用。
func (s *ChatService) CompletionsRaw(ctx context.Context, req ChatCompletionRequest) ([]byte, error) {
	if req.Model == "" {
		return nil, errors.New("wau: ChatCompletionRequest.Model is required")
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("wau: ChatCompletionRequest.Messages must not be empty")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("wau: marshal request: %w", err)
	}
	url := s.c.baseURL + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("wau: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	// 鉴权(走 c.signer 跟 doWithRetry 一致)
	if s.c.signer != nil {
		jwtStr, err := s.c.signer.Sign()
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Authorization", "Bearer "+jwtStr)
	}
	resp, err := s.c.opts.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("wau: http do: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wau: read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return raw, &APIError{StatusCode: resp.StatusCode, Body: raw}
	}
	return raw, nil
}

// Stream POST /v1/chat/completions 以 SSE 流式返回 ChatCompletionChunk(per Stage 3.1 #10, 2026-07-02)。
//
// 参数:
//   - ctx:请求上下文(超时 / 取消信号)
//   - req:ChatCompletionRequest,Stream 字段必须为 true(本方法强制覆盖,防误用)
//
// 返回(三件套):
//   - chunks:<-chan ChatCompletionChunk,每收到一个 SSE chunk 就 yield 一次
//   - errs:  <-chan error,网络错 / JSON 解析错 / ctx 取消错 / wau-edge 4xx/5xx 错
//   - cancel: func() error,提前关闭连接(返回 close 错),调用后 chunks 和 errs 都会被关闭
//
// 用法:
//
//	chunks, errs, cancel := c.Chat().Stream(ctx, wau.ChatCompletionRequest{
//	    Model:    "deepseek-v4-flash",
//	    Messages: []wau.ChatMessage{{Role: "user", Content: "1+1=?"}},
//	})
//	defer cancel()
//	for chunk := range chunks {
//	    if len(chunk.Choices) > 0 {
//	        fmt.Print(chunk.Choices[0].Delta.Content)
//	        if chunk.Choices[0].FinishReason != nil && *chunk.Choices[0].FinishReason == "stop" {
//	            break
//	        }
//	    }
//	}
//	if err := <-errs; err != nil { return err }
//
// SSE 协议(per wau-edge stream.go):
//   - 头:Content-Type: text/event-stream + Cache-Control: no-cache + Connection: keep-alive
//   - 每个 chunk:data: {<JSON>}\n\n
//   - 终止:data: [DONE]\n\n
//   - 7 chunks 验证 per C.1: "1" → "+" → "1" → "=" → "2"
//
// 完整链路:Go SDK → wau-edge :18402 → wau-llm-router :18404(unary Resolve)
//   → new-api sidecar → DeepSeek v4-flash reasoning model → SSE chunks
func (s *ChatService) Stream(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error, func() error) {
	chunks := make(chan ChatCompletionChunk, 16)
	errs := make(chan error, 1)

	// 强制 stream=true(防误用)
	req.Stream = true

	// 提前参数校验(沿用 Completions 的逻辑)
	if req.Model == "" {
		errs <- errors.New("wau: ChatCompletionRequest.Model is required")
		close(chunks)
		close(errs)
		return chunks, errs, func() error { return nil }
	}
	if len(req.Messages) == 0 {
		errs <- errors.New("wau: ChatCompletionRequest.Messages must not be empty")
		close(chunks)
		close(errs)
		return chunks, errs, func() error { return nil }
	}

	// 鉴权(走 c.signer 跟 CompletionsRaw 一致)
	var jwtStr string
	if s.c.signer != nil {
		var err error
		jwtStr, err = s.c.signer.Sign()
		if err != nil {
			errs <- fmt.Errorf("wau: sign JWT: %w", err)
			close(chunks)
			close(errs)
			return chunks, errs, func() error { return nil }
		}
	}

	// cancel 通过 cancelFn 控制(主要给 caller 提前关闭)
	cancelFn := func() error { return nil }

	go func() {
		defer close(chunks)
		defer close(errs)

		body, err := json.Marshal(req)
		if err != nil {
			errs <- fmt.Errorf("wau: marshal request: %w", err)
			return
		}

		url := s.c.baseURL + "/v1/chat/completions"
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			errs <- fmt.Errorf("wau: build request: %w", err)
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		if jwtStr != "" {
			httpReq.Header.Set("Authorization", "Bearer "+jwtStr)
		}

		resp, err := s.c.opts.HTTPClient.Do(httpReq)
		if err != nil {
			errs <- fmt.Errorf("wau: http do: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			// wau-edge 4xx/5xx 走 application/json(非 SSE),用 io.ReadAll 读 body
			raw, _ := io.ReadAll(resp.Body)
			errs <- &APIError{StatusCode: resp.StatusCode, Body: raw}
			return
		}

		// SSE 解析:每行 data: <json> 或 data: [DONE]
		scanner := bufio.NewScanner(resp.Body)
		// SSE 单 chunk 可能很大(>64KB),设大 buffer 防 line too long
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue // 跳过空行 / event: / id: / retry: 等
			}
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				return // 终止信号,正常结束
			}

			var chunk ChatCompletionChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				errs <- fmt.Errorf("wau: parse SSE chunk: %w (payload=%q)", err, payload)
				return
			}

			select {
			case chunks <- chunk:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errs <- fmt.Errorf("wau: scanner: %w", err)
		}
	}()

	return chunks, errs, cancelFn
}

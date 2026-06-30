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
// 流式(SSE):雏形期 M3 §3.7 不实现,留 v0.9.x gap(per M2 §2.6 备注:
//   流式需 bufio.Scanner + 独立 callback,放到 M3 §3.7 续或推 v0.9.x)
//
// 错误码(per wau-edge §2.2 6 错误码):
//   -32001 InsufficientTrust  -32002 AgentNotFound   -32003 TenantMismatch
//   -32004 RateLimited        -32005 ProtocolNotSupported
//   -32600 InvalidRequest     -32601 ModelNotFound(per §2.5 OpenAI 兼容层新增)
package wau

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// Package botwebhook — 通用 Webhook Bot SDK 集成(M1 §2.10 实装,2026-06-28 youhaoxi)。
//
// Stage 1 替换 Stage 0 stub:
//  - HTTPS POST 端点(std net/http,生产可上 TLS)
//  - 静态 API Key 验证(Bearer Token,防时序攻击用 subtle.ConstantTimeCompare)
//  - JSON payload → botcommon.IncomingMessage 归一化
//  - 走 wau-core-kernel 经 wau-go-sdk Client.Tasks().Submit
//  - 5 行接入范本 examples/bot_webhook/main.go
//
// 字段对齐 per D13 拍板:与 wau-channel/adapter + 4 SDK bot/common/ 100% 一致。
package botwebhook

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	wau "github.com/wau/wau-go-sdk"
	"github.com/wau/wau-go-sdk/bot/common"
)

// 业务常量
const (
	// userFacingErrPrefix 0 门槛 UX 错误统一前缀
	userFacingErrPrefix = "暂时无法回复,请稍后再试"

	// defaultTimeoutMs SubmitTask 默认超时
	defaultTimeoutMs = 30000

	// maxPayloadBytes 限制 payload 大小(防 DoS,1 MiB)
	maxPayloadBytes = 1 << 20
)

// webhookPayload Webhook 接收的 JSON payload 格式。
type webhookPayload struct {
	PlatformMsgID string `json:"platform_msg_id,omitempty"`
	ChannelID     string `json:"channel_id"`
	UserID        string `json:"user_id"`
	Username      string `json:"username,omitempty"`
	Text          string `json:"text"`
	ReplyTo       string `json:"reply_to,omitempty"`
}

// webhookResponse Webhook 返回给上游的响应。
type webhookResponse struct {
	OK        bool   `json:"ok"`
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"user_facing_error,omitempty"`
}

// WebhookBot Webhook Bot SDK 集成主结构。
type WebhookBot struct {
	Addr     string
	Tenant   string
	Universe string
	Handler  func(botcommon.IncomingMessage) botcommon.OutgoingMessage

	client  *wau.Client
	apiKey  string
	server  *http.Server
	mu      sync.Mutex
	stopped chan struct{}
}

// New 用 addr + builder 创建 Webhook bot。
func New(addr string, builder *botcommon.BotBuilder) *WebhookBot {
	return &WebhookBot{
		Addr:     addr,
		Tenant:   builder.TenantID(),
		Universe: builder.Universe(),
		Handler:  builder.Handler(),
		stopped:  make(chan struct{}),
	}
}

// NewWithClient 测试用构造函数(注入 mock client)。
func NewWithClient(addr string, builder *botcommon.BotBuilder, client *wau.Client, apiKey string) *WebhookBot {
	bot := New(addr, builder)
	bot.client = client
	bot.apiKey = apiKey
	return bot
}

// SetAPIKey 设置静态 API Key(可选,空 = 不鉴权)。
func (w *WebhookBot) SetAPIKey(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.apiKey = key
}

// Start 启动 HTTP server 监听 Webhook 端点。
func (w *WebhookBot) Start(_ context.Context) error {
	w.mu.Lock()
	if w.server != nil {
		w.mu.Unlock()
		return errors.New("botwebhook: already started")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/bot/webhook/incoming", w.handleIncoming)
	mux.HandleFunc("/v1/bot/webhook/health", w.handleHealth)

	w.server = &http.Server{
		Addr:              w.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	w.stopped = make(chan struct{})
	w.mu.Unlock()

	// 后台 ListenAndServe
	go func() {
		if err := w.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[botwebhook] ListenAndServe: %v", err)
		}
	}()

	log.Printf("[botwebhook] started: addr=%q tenant=%q", w.Addr, w.Tenant)
	return nil
}

// StartWithListener 用 caller 提供的 listener 启动(测试用,避免 :0 端口冲突)。
func (w *WebhookBot) StartWithListener(ln net.Listener) error {
	w.mu.Lock()
	if w.server != nil {
		w.mu.Unlock()
		return errors.New("botwebhook: already started")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/bot/webhook/incoming", w.handleIncoming)
	mux.HandleFunc("/v1/bot/webhook/health", w.handleHealth)

	w.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	w.stopped = make(chan struct{})
	w.mu.Unlock()

	go func() {
		if err := w.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[botwebhook] Serve: %v", err)
		}
	}()

	log.Printf("[botwebhook] started: addr=%q tenant=%q", ln.Addr().String(), w.Tenant)
	return nil
}

// Stop 优雅停止 HTTP server。
func (w *WebhookBot) Stop(_ context.Context) error {
	w.mu.Lock()
	srv := w.server
	if srv == nil {
		w.mu.Unlock()
		return nil
	}
	select {
	case <-w.stopped:
		w.mu.Unlock()
		return nil
	default:
	}
	close(w.stopped)
	w.mu.Unlock()

	// Shutdown 在锁外调用(避免 shutdown 阻塞时锁被占用)
	if err := srv.Shutdown(context.Background()); err != nil {
		log.Printf("[botwebhook] Shutdown: %v", err)
	}
	// 等 Serve goroutine 完全退出(避免 nil 访问)
	time.Sleep(10 * time.Millisecond)

	w.mu.Lock()
	w.server = nil
	w.mu.Unlock()

	log.Printf("[botwebhook] stopped")
	return nil
}

// OnMessage 注册消息处理 handler,返回 Bot 支持链式调用。
func (w *WebhookBot) OnMessage(handler func(botcommon.IncomingMessage) botcommon.OutgoingMessage) botcommon.Bot {
	w.Handler = handler
	return w
}

// WithTenant 设置 tenant_id。
func (w *WebhookBot) WithTenant(tenantID string) botcommon.Bot {
	w.Tenant = tenantID
	return w
}

// WithUniverse 设置 Universe 标签。
func (w *WebhookBot) WithUniverse(universe string) botcommon.Bot {
	w.Universe = universe
	return w
}

// handleIncoming 处理 Webhook POST 入口。
func (w *WebhookBot) handleIncoming(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. API Key 验证(可选)
	w.mu.Lock()
	apiKey := w.apiKey
	w.mu.Unlock()
	if apiKey != "" {
		auth := r.Header.Get("Authorization")
		expected := "Bearer " + apiKey
		if subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) != 1 {
			respondJSON(rw, http.StatusUnauthorized, webhookResponse{OK: false, Error: userFacingErrPrefix})
			return
		}
	}

	// 2. 读取 + 解析 body(限大小)
	body, err := io.ReadAll(http.MaxBytesReader(rw, r.Body, maxPayloadBytes))
	if err != nil {
		respondJSON(rw, http.StatusBadRequest, webhookResponse{OK: false, Error: userFacingErrPrefix})
		return
	}
	var p webhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		respondJSON(rw, http.StatusBadRequest, webhookResponse{OK: false, Error: userFacingErrPrefix})
		return
	}
	if p.ChannelID == "" || p.UserID == "" {
		respondJSON(rw, http.StatusBadRequest, webhookResponse{OK: false, Error: userFacingErrPrefix})
		return
	}

	// 3. handler 处理
	if w.Handler == nil {
		respondJSON(rw, http.StatusServiceUnavailable, webhookResponse{OK: false, Error: userFacingErrPrefix})
		return
	}
	in := botcommon.IncomingMessage{
		PlatformMsgID: p.PlatformMsgID,
		ChannelID:     p.ChannelID,
		UserID:        p.UserID,
		Username:      p.Username,
		Text:          p.Text,
		ReplyTo:       p.ReplyTo,
		Timestamp:     time.Now().UTC(),
	}
	out := w.Handler(in)

	// 4. 返回响应
	if out.Text == "" {
		respondJSON(rw, http.StatusOK, webhookResponse{OK: true})
		return
	}
	respondJSON(rw, http.StatusOK, webhookResponse{OK: true, MessageID: "msg-" + p.ChannelID})
}

// handleHealth 健康检查端点。
func (w *WebhookBot) handleHealth(rw http.ResponseWriter, _ *http.Request) {
	w.mu.Lock()
	running := w.server != nil
	w.mu.Unlock()
	if !running {
		http.Error(rw, "not running", http.StatusServiceUnavailable)
		return
	}
	respondJSON(rw, http.StatusOK, map[string]string{"status": "ok"})
}

// SubmitToCore 暴露的 helper:用 wau-go-sdk Client 提交 task 给 wau-core(per wau-core L4 协议)。
func (w *WebhookBot) SubmitToCore(ctx context.Context, prompt string) (string, error) {
	if w.client == nil {
		return "", fmt.Errorf("botwebhook: %s", userFacingErrPrefix)
	}
	resp, err := w.client.Tasks().Submit(ctx, wau.SubmitRequest{
		Prompt:    prompt,
		TimeoutMs: defaultTimeoutMs,
	})
	if err != nil {
		log.Printf("[botwebhook] Submit: %v", err)
		return "", fmt.Errorf("botwebhook: %s", userFacingErrPrefix)
	}
	if resp.Error != "" {
		log.Printf("[botwebhook] kernel error: %s", resp.Error)
		return "", fmt.Errorf("botwebhook: %s", userFacingErrPrefix)
	}
	return fmt.Sprintf("%v", resp.Response), nil
}

// respondJSON 写 JSON 响应。
func respondJSON(rw http.ResponseWriter, status int, body any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	_ = json.NewEncoder(rw).Encode(body)
}

// 编译期接口断言
var _ botcommon.Bot = (*WebhookBot)(nil)

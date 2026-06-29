// Package bottelegram — Telegram Bot SDK 集成(M1 §2.8 实装,2026-06-28 youhaoxi)。
//
// Stage 1 替换 Stage 0 stub:
//   - 接入 github.com/go-telegram-bot-api/telegram-bot-api/v5(已 v5.5.1 验证 wau-channel 仓)
//   - Long Polling(默认)或 setWebhook
//   - 走 wau-core-kernel 经 wau-go-sdk Client.Tasks().Submit
//   - 5 行接入范本 examples/bot_telegram.go
//
// 字段对齐 per D13 拍板:与 wau-channel/adapter + 4 SDK bot/common/ 100% 一致。
package bottelegram

import (
	"context"
	"fmt"
	"log"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	wau "github.com/wau/wau-go-sdk"
	"github.com/wau/wau-go-sdk/bot/common"
)

// 业务常量
const (
	// userFacingErrPrefix 0 门槛 UX 错误统一前缀
	userFacingErrPrefix = "暂时无法回复,请稍后再试"

	// defaultTimeoutMs SubmitTask 默认超时
	defaultTimeoutMs = 30000

	// defaultPollingTimeout Long Polling 单次超时(Telegram 推荐 ~10s)
	defaultPollingTimeout = 10
)

// TelegramBot Telegram Bot SDK 集成主结构。
type TelegramBot struct {
	Token    string
	Tenant   string
	Universe string
	Handler  func(botcommon.IncomingMessage) botcommon.OutgoingMessage

	client  *wau.Client
	api     *tgbotapi.BotAPI
	tgAPI   TelegramAPI
	updates tgbotapi.UpdatesChannel

	mu      sync.Mutex
	stopped chan struct{}
}

// New 用 token + builder 创建 Telegram bot。
func New(token string, builder *botcommon.BotBuilder) *TelegramBot {
	return &TelegramBot{
		Token:    token,
		Tenant:   builder.TenantID(),
		Universe: builder.Universe(),
		Handler:  builder.Handler(),
		stopped:  make(chan struct{}),
	}
}

// NewWithClient 测试用构造函数(注入 mock client + mock API)。
func NewWithClient(token string, builder *botcommon.BotBuilder, client *wau.Client, api TelegramAPI) *TelegramBot {
	bot := New(token, builder)
	bot.client = client
	bot.tgAPI = api
	return bot
}

// TelegramAPI Telegram API 抽象(只暴露 Bot API 必需方法,测试可 mock)。
type TelegramAPI interface {
	GetUpdatesChan(config tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel
	StopReceivingUpdates()
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}

// tgAPI 实际 tgbotapi 适配(避免暴露字段冲突)
type tgAPI struct{ api *tgbotapi.BotAPI }

func (t tgAPI) GetUpdatesChan(c tgbotapi.UpdateConfig) tgbotapi.UpdatesChannel {
	return t.api.GetUpdatesChan(c)
}
func (t tgAPI) StopReceivingUpdates() { t.api.StopReceivingUpdates() }
func (t tgAPI) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	return t.api.Send(c)
}

// Start 启动 bot(Long Polling 默认)。
func (t *TelegramBot) Start(ctx context.Context) error {
	if t.tgAPI == nil {
		// 生产路径:连接真实 Telegram
		api, err := tgbotapi.NewBotAPI(t.Token)
		if err != nil {
			log.Printf("[bottelegram] NewBotAPI: %v", err)
			return fmt.Errorf("bottelegram: %s", userFacingErrPrefix)
		}
		t.api = api
		t.tgAPI = tgAPI{api: api}
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = defaultPollingTimeout
	updates := t.tgAPI.GetUpdatesChan(u)
	t.updates = updates

	// 后台 goroutine 处理 update
	go t.handleUpdates(ctx)

	log.Printf("[bottelegram] started: tenant=%q universe=%q", t.Tenant, t.Universe)
	return nil
}

// Stop 优雅停止 bot。
func (t *TelegramBot) Stop(_ context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case <-t.stopped:
		return nil // 已停止
	default:
	}
	close(t.stopped)
	if t.tgAPI != nil {
		t.tgAPI.StopReceivingUpdates()
	}
	log.Printf("[bottelegram] stopped")
	return nil
}

// OnMessage 注册消息处理 handler,返回 Bot 支持链式调用。
func (t *TelegramBot) OnMessage(handler func(botcommon.IncomingMessage) botcommon.OutgoingMessage) botcommon.Bot {
	t.Handler = handler
	return t
}

// WithTenant 设置 tenant_id。
func (t *TelegramBot) WithTenant(tenantID string) botcommon.Bot {
	t.Tenant = tenantID
	return t
}

// WithUniverse 设置 Universe 标签。
func (t *TelegramBot) WithUniverse(universe string) botcommon.Bot {
	t.Universe = universe
	return t
}

// handleUpdates 后台循环处理 Telegram update。
func (t *TelegramBot) handleUpdates(ctx context.Context) {
	for {
		select {
		case <-t.stopped:
			return
		case <-ctx.Done():
			return
		case upd, ok := <-t.updates:
			if !ok {
				return
			}
			if upd.Message == nil {
				continue
			}
			t.processMessage(ctx, upd.Message)
		}
	}
}

// processMessage 单条消息处理(IncomingMessage → handler → OutgoingMessage → Reply)。
func (t *TelegramBot) processMessage(ctx context.Context, m *tgbotapi.Message) {
	if t.Handler == nil {
		log.Printf("[bottelegram] no handler, dropping message from chat %d", m.Chat.ID)
		return
	}

	in := botcommon.IncomingMessage{
		PlatformMsgID: fmt.Sprintf("%d", m.MessageID),
		ChannelID:     fmt.Sprintf("%d", m.Chat.ID),
		UserID:        fmt.Sprintf("%d", m.From.ID),
		Username:      m.From.UserName,
		Text:          m.Text,
		ReplyTo:       replyToID(m),
	}

	// 调 handler(用户自己写),拿到 OutgoingMessage
	out := t.Handler(in)

	// 0 门槛 UX:handler 返回空 Text 不发
	if out.Text == "" {
		return
	}

	// Reply 消息
	reply := tgbotapi.NewMessage(m.Chat.ID, out.Text)
	reply.ReplyToMessageID = m.MessageID
	if _, err := t.tgAPI.Send(reply); err != nil {
		log.Printf("[bottelegram] send reply: %v", err)
	}
}

// replyToID 提取 reply_to 消息 ID(per D13 字段对齐)。
func replyToID(m *tgbotapi.Message) string {
	if m.ReplyToMessage == nil {
		return ""
	}
	return fmt.Sprintf("%d", m.ReplyToMessage.MessageID)
}

// SubmitToCore 暴露的 helper:用 wau-go-sdk Client 提交 task 给 wau-core(per wau-core L4 协议)。
//
//	这是 SDK 端的主链路,handler 调它返回 SubmitResponse.Response 文本。
func (t *TelegramBot) SubmitToCore(ctx context.Context, prompt string) (string, error) {
	if t.client == nil {
		return "", fmt.Errorf("bottelegram: %s", userFacingErrPrefix)
	}
	resp, err := t.client.Tasks().Submit(ctx, wau.SubmitRequest{
		Prompt:    prompt,
		TimeoutMs: defaultTimeoutMs,
	})
	if err != nil {
		log.Printf("[bottelegram] Submit: %v", err)
		return "", fmt.Errorf("bottelegram: %s", userFacingErrPrefix)
	}
	if resp.Error != "" {
		log.Printf("[bottelegram] kernel error: %s", resp.Error)
		return "", fmt.Errorf("bottelegram: %s", userFacingErrPrefix)
	}
	// resp.Response 是 any,简化用 fmt.Sprintf
	return fmt.Sprintf("%v", resp.Response), nil
}

// 编译期接口断言
var _ botcommon.Bot = (*TelegramBot)(nil)

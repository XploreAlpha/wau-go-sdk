// Package botdiscord — Discord Bot SDK 集成(M1 §2.9 实装,2026-06-28 youhaoxi)。
//
// Stage 1 替换 Stage 0 stub:
//  - 接入 github.com/bwmarrin/discordgo v0.29.0(已 wau-channel 仓验证)
//  - Bot Gateway WebSocket + REST API
//  - 走 wau-core-kernel 经 wau-go-sdk Client.Tasks().Submit
//  - 5 行接入范本 examples/bot_discord/main.go
//
// 字段对齐 per D13 拍板:与 wau-channel/adapter + 4 SDK bot/common/ 100% 一致。
package botdiscord

import (
	"context"
	"fmt"
	"log"
	"sync"

	discordgo "github.com/bwmarrin/discordgo"

	wau "github.com/wau/wau-go-sdk"
	"github.com/wau/wau-go-sdk/bot/common"
)

// 业务常量
const (
	// userFacingErrPrefix 0 门槛 UX 错误统一前缀
	userFacingErrPrefix = "暂时无法回复,请稍后再试"

	// defaultTimeoutMs SubmitTask 默认超时
	defaultTimeoutMs = 30000

	// mentionPrefixUser Discord user mention 格式(per Discord API)
	mentionPrefixUser = "<@"
)

// DiscordBot Discord Bot SDK 集成主结构。
type DiscordBot struct {
	Token    string
	Tenant   string
	Universe string
	Handler  func(botcommon.IncomingMessage) botcommon.OutgoingMessage

	client *wau.Client
	dg     *discordgo.Session
	dgAPI  DiscordAPI
	mu     sync.Mutex
	stopped chan struct{}

	// bot 自身信息(由 Start 时 lazy 拿)
	botID string
}

// New 用 token + builder 创建 Discord bot。
func New(token string, builder *botcommon.BotBuilder) *DiscordBot {
	return &DiscordBot{
		Token:    token,
		Tenant:   builder.TenantID(),
		Universe: builder.Universe(),
		Handler:  builder.Handler(),
		stopped:  make(chan struct{}),
	}
}

// NewWithClient 测试用构造函数(注入 mock client + mock DiscordAPI)。
func NewWithClient(token string, builder *botcommon.BotBuilder, client *wau.Client, api DiscordAPI) *DiscordBot {
	bot := New(token, builder)
	bot.client = client
	bot.dgAPI = api
	bot.botID = "self-bot-id" // mock 模式默认设
	return bot
}

// DiscordAPI Discord session 抽象(只暴露 Bot 必需方法,测试可 mock)。
type DiscordAPI interface {
	Open() error
	Close() error
	ChannelMessageSend(channelID, content string, options ...discordgo.RequestOption) (*discordgo.Message, error)
	AddHandler(handler interface{}) func()
}

// dgAPI 实际 discordgo 适配(避免暴露字段冲突)
type dgAPI struct{ s *discordgo.Session }

func (d dgAPI) Open() error                              { return d.s.Open() }
func (d dgAPI) Close() error                             { return d.s.Close() }
func (d dgAPI) ChannelMessageSend(c, m string, o ...discordgo.RequestOption) (*discordgo.Message, error) {
	return d.s.ChannelMessageSend(c, m, o...)
}
func (d dgAPI) AddHandler(h interface{}) func() { return d.s.AddHandler(h) }

// Start 启动 bot(Bot Gateway WebSocket)。
func (d *DiscordBot) Start(ctx context.Context) error {
	if d.dgAPI == nil {
		// 生产路径:连接真实 Discord Gateway
		dg, err := discordgo.New("Bot " + d.Token)
		if err != nil {
			log.Printf("[botdiscord] New: %v", err)
			return fmt.Errorf("botdiscord: %s", userFacingErrPrefix)
		}
		d.dg = dg
		d.dgAPI = dgAPI{s: dg}

		// lazy 拿 bot 自身 ID(失败时降级为空)
		if u, err := dg.User("@me"); err == nil {
			d.botID = u.ID
		} else {
			log.Printf("[botdiscord] fetch self user failed: %v", err)
		}
	}

	if err := d.dgAPI.Open(); err != nil {
		log.Printf("[botdiscord] Open: %v", err)
		return fmt.Errorf("botdiscord: %s", userFacingErrPrefix)
	}

	// 注册 MESSAGE_CREATE handler
	d.dgAPI.AddHandler(d.onMessageCreate)

	log.Printf("[botdiscord] started: tenant=%q universe=%q", d.Tenant, d.Universe)
	return nil
}

// Stop 优雅停止 bot。
func (d *DiscordBot) Stop(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	select {
	case <-d.stopped:
		return nil
	default:
	}
	close(d.stopped)
	if d.dgAPI != nil {
		if err := d.dgAPI.Close(); err != nil {
			log.Printf("[botdiscord] Close: %v", err)
		}
	}
	log.Printf("[botdiscord] stopped")
	return nil
}

// OnMessage 注册消息处理 handler,返回 Bot 支持链式调用。
func (d *DiscordBot) OnMessage(handler func(botcommon.IncomingMessage) botcommon.OutgoingMessage) botcommon.Bot {
	d.Handler = handler
	return d
}

// WithTenant 设置 tenant_id。
func (d *DiscordBot) WithTenant(tenantID string) botcommon.Bot {
	d.Tenant = tenantID
	return d
}

// WithUniverse 设置 Universe 标签。
func (d *DiscordBot) WithUniverse(universe string) botcommon.Bot {
	d.Universe = universe
	return d
}

// onMessageCreate Discord MESSAGE_CREATE event handler。
func (d *DiscordBot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m == nil || m.Message == nil {
		return
	}
	// 忽略 bot 自己发的消息
	if d.botID != "" && m.Author.ID == d.botID {
		return
	}
	if d.Handler == nil {
		log.Printf("[botdiscord] no handler, dropping message from %s", m.Author.Username)
		return
	}

	in := botcommon.IncomingMessage{
		PlatformMsgID: m.ID,
		ChannelID:     m.ChannelID,
		UserID:        m.Author.ID,
		Username:      m.Author.Username,
		Text:          stripBotMention(m.Content, d.botID),
		ReplyTo:       replyToID(m.Message),
	}

	// 调 handler 拿 OutgoingMessage
	out := d.Handler(in)
	if out.Text == "" {
		return
	}

	if _, err := d.dgAPI.ChannelMessageSend(m.ChannelID, out.Text); err != nil {
		log.Printf("[botdiscord] send reply: %v", err)
	}
}

// stripBotMention 移除 Discord <@BOT_ID> / <@!BOT_ID> mention 前缀。
func stripBotMention(text, botID string) string {
	if botID == "" {
		return text
	}
	// 简化:只剥 user 版 <@ID> + nickname 版 <@!ID>(per wau-channel/adapter/discord 同模式)
	for _, prefix := range []string{mentionPrefixUser, "<@!"} {
		marker := prefix + botID + ">"
		if len(text) >= len(marker) && text[:len(marker)] == marker {
			text = text[len(marker):]
			// 跳过空格
			for len(text) > 0 && (text[0] == ' ' || text[0] == '\t') {
				text = text[1:]
			}
			return text
		}
	}
	return text
}

// replyToID 提取 reply_to 消息 ID(per D13 字段对齐)。
func replyToID(m *discordgo.Message) string {
	if m.ReferencedMessage == nil {
		return ""
	}
	return m.ReferencedMessage.ID
}

// SubmitToCore 暴露的 helper:用 wau-go-sdk Client 提交 task 给 wau-core(per wau-core L4 协议)。
func (d *DiscordBot) SubmitToCore(ctx context.Context, prompt string) (string, error) {
	if d.client == nil {
		return "", fmt.Errorf("botdiscord: %s", userFacingErrPrefix)
	}
	resp, err := d.client.Tasks().Submit(ctx, wau.SubmitRequest{
		Prompt:    prompt,
		TimeoutMs: defaultTimeoutMs,
	})
	if err != nil {
		log.Printf("[botdiscord] Submit: %v", err)
		return "", fmt.Errorf("botdiscord: %s", userFacingErrPrefix)
	}
	if resp.Error != "" {
		log.Printf("[botdiscord] kernel error: %s", resp.Error)
		return "", fmt.Errorf("botdiscord: %s", userFacingErrPrefix)
	}
	return fmt.Sprintf("%v", resp.Response), nil
}

// 编译期接口断言
var _ botcommon.Bot = (*DiscordBot)(nil)

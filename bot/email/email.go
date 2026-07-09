// Package botemail — Email Bot SDK 集成(W6.2 Stage 1 native SDK integration, 2026-07-09)。
//
// Stage 1 native 实现(替换 W5 Stage 0 stub):
//   - 收件:github.com/emersion/go-imap v1(IMAP client + IDLE 主动推送)
//   - 发件:net/smtp(stdlib,不引额外 dep)
//   - 公共 Bot interface 沿用 M10 N1 拍板(Start/Stop/OnMessage/WithTenant/WithUniverse)
//   - SubmitToCore 走 wau-core-kernel wau.Client.Tasks().Submit(per W7 模板)
//
// IDLE 语义(go-imap v1 协议约束):IMAP 不允许 IDLE 期间 Fetch,
// 故收到 MailboxUpdate 后先停 IDLE → 增量 Fetch → 重进 IDLE。
// Email 无 update 概念:UpdateMessage 保留为兼容接口(no-op 语义)。
//
// 协议合规(per wau-channel/adapter/email/email_real.go W7 2026-07-07 SDK 接通):
//   - D60 additive:老 bot/ 子包(telegram/discord/webhook)0 改动
//   - 公共 Bot interface 5 方法签名一字不改
package botemail

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/smtp"
	"strings"
	"sync"
	"time"

	imap "github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"

	wau "github.com/wau/wau-go-sdk"
	botcommon "github.com/wau/wau-go-sdk/bot/common"
)

const (
	userFacingErrPrefix = "暂时无法回复,请稍后再试"
	defaultTimeoutMs    = 30000
	rePrefix            = "Re: "
)

// EmailBot Email Bot SDK 集成主结构(Stage 1 native)。
type EmailBot struct {
	IMAPHost string
	IMAPPort int
	IMAPUser string
	IMAPPass string
	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	Tenant   string
	Universe string
	Handler  func(botcommon.IncomingMessage) botcommon.OutgoingMessage
	client   *wau.Client

	// native SDK 句柄(Start 时构造)
	imapConn  *imapclient.Client
	lastCount uint32

	mu      sync.Mutex
	running bool
	stop    chan struct{}
	stopped chan struct{}
}

var _ botcommon.Bot = (*EmailBot)(nil)

// NewEmailBot 创建 Email Bot 实例。
func NewEmailBot(imapHost string, imapPort int, imapUser, imapPass, smtpHost string, smtpPort int, smtpUser, smtpPass string) *EmailBot {
	return &EmailBot{
		IMAPHost: imapHost,
		IMAPPort: imapPort,
		IMAPUser: imapUser,
		IMAPPass: imapPass,
		SMTPHost: smtpHost,
		SMTPPort: smtpPort,
		SMTPUser: smtpUser,
		SMTPPass: smtpPass,
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
}

// WithClient 注入 wau Client。
func (b *EmailBot) WithClient(c *wau.Client) *EmailBot {
	b.client = c
	return b
}

// WithTenant 设置 tenant_id。
func (b *EmailBot) WithTenant(tenantID string) botcommon.Bot {
	b.Tenant = tenantID
	return b
}

// WithUniverse 设置 Universe 标签。
func (b *EmailBot) WithUniverse(universe string) botcommon.Bot {
	b.Universe = universe
	return b
}

// OnMessage 注册消息处理 handler。
func (b *EmailBot) OnMessage(handler func(botcommon.IncomingMessage) botcommon.OutgoingMessage) botcommon.Bot {
	b.Handler = handler
	return b
}

// Start 连接 IMAP(TLS)+ Login + Select INBOX + 启 IDLE goroutine。
func (b *EmailBot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("email bot already running")
	}
	if b.IMAPHost == "" || b.IMAPUser == "" {
		b.mu.Unlock()
		return fmt.Errorf("botemail: imap host/user required")
	}
	b.mu.Unlock()

	addr := fmt.Sprintf("%s:%d", b.IMAPHost, b.IMAPPort)
	conn, err := imapclient.DialTLS(addr, nil)
	if err != nil {
		return fmt.Errorf("botemail: imap dial failed: %w", err)
	}
	if err := conn.Login(b.IMAPUser, b.IMAPPass); err != nil {
		_ = conn.Logout()
		return fmt.Errorf("botemail: imap login failed: %w", err)
	}
	status, err := conn.Select("INBOX", false)
	if err != nil {
		_ = conn.Logout()
		return fmt.Errorf("botemail: imap select failed: %w", err)
	}

	b.mu.Lock()
	b.imapConn = conn
	b.lastCount = status.Messages
	b.running = true
	b.mu.Unlock()

	go b.idleLoop()
	return nil
}

// idleLoop 后台 IMAP IDLE:收到新邮件计数变化 → 停 IDLE → 增量 fetch → 重启 IDLE。
func (b *EmailBot) idleLoop() {
	defer close(b.stopped)
	b.mu.Lock()
	conn := b.imapConn
	b.mu.Unlock()
	if conn == nil {
		return
	}
	updates := make(chan imapclient.Update, 16)
	conn.Updates = updates

	for {
		idleStop := make(chan struct{})
		idleDone := make(chan error, 1)
		go func() { idleDone <- conn.Idle(idleStop, nil) }()

		var newCount uint32
		var haveNew bool
		select {
		case <-b.stop:
			close(idleStop)
			<-idleDone
			return
		case upd := <-updates:
			if mu, ok := upd.(*imapclient.MailboxUpdate); ok && mu.Mailbox != nil {
				newCount = mu.Mailbox.Messages
				haveNew = true
			}
			close(idleStop)
			<-idleDone
		case err := <-idleDone:
			if err != nil {
				log.Printf("[botemail] imap idle ended: %v", err)
			}
			return
		}
		if haveNew {
			b.fetchNew(newCount)
		}
	}
}

// fetchNew 增量拉取 [lastCount+1, newCount],解析后 dispatch。
func (b *EmailBot) fetchNew(newCount uint32) {
	b.mu.Lock()
	conn := b.imapConn
	from := b.lastCount + 1
	b.mu.Unlock()
	if conn == nil || newCount < from {
		return
	}
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, newCount)
	section := &imap.BodySectionName{BodyPartName: imap.BodyPartName{Specifier: imap.TextSpecifier}}
	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchInternalDate, section.FetchItem()}

	messages := make(chan *imap.Message, 16)
	fetchDone := make(chan error, 1)
	go func() { fetchDone <- conn.Fetch(seqset, items, messages) }()

	for msg := range messages {
		b.dispatch(msg, section)
	}
	if err := <-fetchDone; err != nil {
		log.Printf("[botemail] imap fetch error: %v", err)
	}

	b.mu.Lock()
	if newCount > b.lastCount {
		b.lastCount = newCount
	}
	b.mu.Unlock()
}

// dispatch 归一化 IMAP 邮件 → IncomingMessage → Handler → SendMessage 回复。
func (b *EmailBot) dispatch(msg *imap.Message, section *imap.BodySectionName) {
	if b.Handler == nil || msg == nil || msg.Envelope == nil {
		return
	}
	env := msg.Envelope
	in := botcommon.IncomingMessage{
		PlatformMsgID: env.MessageId,
		ReplyTo:       env.InReplyTo,
		Text:          env.Subject,
		Timestamp:     env.Date,
	}
	if in.Timestamp.IsZero() {
		in.Timestamp = msg.InternalDate
	}
	if len(env.From) > 0 && env.From[0] != nil {
		in.UserID = addressString(env.From[0])
		in.ChannelID = in.UserID
		in.Username = env.From[0].PersonalName
	}
	if lit := msg.GetBody(section); lit != nil {
		if body, err := io.ReadAll(lit); err == nil {
			in.Text = strings.TrimSpace(string(body))
		}
	}
	out := b.Handler(in)
	if out.Text == "" || in.UserID == "" {
		return
	}
	subject := env.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	_ = b.sendReply(in.UserID, subject, out.Text, env.MessageId)
}

// Stop 停 IDLE 循环 + Logout IMAP。
func (b *EmailBot) Stop(ctx context.Context) error {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return nil
	}
	b.running = false
	conn := b.imapConn
	b.mu.Unlock()

	close(b.stop)
	select {
	case <-b.stopped:
	case <-time.After(5 * time.Second):
		log.Printf("[botemail] stop: idle loop timeout")
	}
	if conn != nil {
		return conn.Logout()
	}
	return nil
}

// SendMessage 用 net/smtp 发送邮件(SMTP PLAIN 认证)。Email 用 SendMessage 而非 PostMessage。
func (b *EmailBot) SendMessage(ctx context.Context, to, subject, body string) error {
	if !b.running {
		return fmt.Errorf("email bot not running")
	}
	return b.sendReply(to, subject, body, "")
}

// UpdateMessage Email 不支持更新已发邮件,保留为兼容接口。
func (b *EmailBot) UpdateMessage(ctx context.Context, messageID, newBody string) error {
	if !b.running {
		return fmt.Errorf("email bot not running")
	}
	// Email 无 update 语义:no-op(维持 5-method interface 兼容)
	return nil
}

// SubmitToCore 走 wau-core-kernel 提交 prompt(per W7 模板)。
func (b *EmailBot) SubmitToCore(ctx context.Context, prompt string) (string, error) {
	if b.client == nil {
		return "", fmt.Errorf("wau client not set; call WithClient first")
	}
	resp, err := b.client.Tasks().Submit(ctx, wau.SubmitRequest{
		Prompt:    prompt,
		TimeoutMs: defaultTimeoutMs,
	})
	if err != nil {
		return "", fmt.Errorf("botemail: %s", userFacingErrPrefix)
	}
	return resp.TaskID, nil
}

// --- helpers ---

// sendReply 用 net/smtp 发送邮件(inReplyTo 非空 → 维持 thread 链路)。
func (b *EmailBot) sendReply(to, subject, body, inReplyTo string) error {
	if to == "" {
		return fmt.Errorf("botemail: empty recipient")
	}
	auth := smtp.PlainAuth("", b.SMTPUser, b.SMTPPass, b.SMTPHost)
	addr := fmt.Sprintf("%s:%d", b.SMTPHost, b.SMTPPort)
	from := b.SMTPUser
	msg := buildRFC2822(from, to, subject, body, inReplyTo)
	if err := smtp.SendMail(addr, auth, from, []string{to}, msg); err != nil {
		return fmt.Errorf("botemail: %s", userFacingErrPrefix)
	}
	return nil
}

// buildRFC2822 拼一封最小 RFC 2822 邮件(header + 空行 + body)。
func buildRFC2822(from, to, subject, body, inReplyTo string) []byte {
	if subject == "" {
		subject = "(no subject)"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "From: %s\r\n", from)
	fmt.Fprintf(&sb, "To: %s\r\n", to)
	if inReplyTo != "" {
		if len(subject) < 3 || !strings.EqualFold(subject[:3], "Re:") {
			subject = rePrefix + subject
		}
		fmt.Fprintf(&sb, "Subject: %s\r\n", subject)
		fmt.Fprintf(&sb, "In-Reply-To: %s\r\n", inReplyTo)
		fmt.Fprintf(&sb, "References: %s\r\n", inReplyTo)
	} else {
		fmt.Fprintf(&sb, "Subject: %s\r\n", subject)
	}
	fmt.Fprintf(&sb, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&sb, "Content-Type: text/plain; charset=UTF-8\r\n")
	fmt.Fprintf(&sb, "\r\n")
	sb.WriteString(body)
	return []byte(sb.String())
}

// addressString 把 IMAP Address 拼成 mailbox@host 邮箱串。
func addressString(a *imap.Address) string {
	if a == nil {
		return ""
	}
	if a.MailboxName == "" || a.HostName == "" {
		return a.MailboxName
	}
	return a.MailboxName + "@" + a.HostName
}

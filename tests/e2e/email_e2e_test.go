// email_e2e_test.go — Email Bot SDK mock end-to-end tests (W7.2 closure, 2026-07-09).
//
// 3 cases per spec: success / APIErr / auth_fail.
//
// Strategy:
//   - net.Listen("tcp", 127.0.0.1:0) starts an in-process SMTP server on a random port.
//   - Minimal SMTP dialogue (HELO/MAIL/RCPT/DATA/QUIT) — captured body asserted via atomic.Value.
//   - Construct botemail.EmailBot via public constructor NewEmailBot (dummy IMAP/SMTP creds).
//   - Use reflection to set `running = true` so SendMessage passes the gate.
//     IMAP connection is not exercised in these tests (we only verify SMTP send path).
//   - SendMessage → buildRFC2822 → smtp.SendMail(addr, auth, from, []string{to}, msg).
//
// All tests use atomic counters and run with 0 creds.
package e2e

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	botemail "github.com/wau/wau-go-sdk/bot/email"
)

// smtpCapture records what an SMTP session received.
type smtpCapture struct {
	from string
	rcpt string
	body string
}

// smtpMockServer minimal in-process SMTP server (per wau-channel/adapter/email pattern).
// Accepts one connection, performs the SMTP handshake, captures the message, replies 250 ok.
type smtpMockServer struct {
	ln      net.Listener
	addr    string
	done    chan smtpCapture
	once    sync.Once
	called  atomic.Int32
	lastErr atomic.Value // string — last error encountered
}

func startMockSMTP(t *testing.T) *smtpMockServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("mock SMTP listen: %v", err)
	}
	s := &smtpMockServer{
		ln:   ln,
		addr: ln.Addr().String(),
		done: make(chan smtpCapture, 1),
	}
	go s.serve()
	return s
}

func (s *smtpMockServer) serve() {
	conn, err := s.ln.Accept()
	if err != nil {
		s.lastErr.Store(err.Error())
		return
	}
	defer conn.Close()
	s.called.Add(1)
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	writeLine := func(l string) {
		_, _ = w.WriteString(l + "\r\n")
		_ = w.Flush()
	}

	var cap smtpCapture
	writeLine("220 mock ESMTP")
	inData := false
	var bodyBuf bytes.Buffer
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			s.lastErr.Store(err.Error())
			return
		}
		trimmed := strings.TrimRight(line, "\r\n")

		if inData {
			if trimmed == "." {
				inData = false
				cap.body = bodyBuf.String()
				writeLine("250 ok queued")
				continue
			}
			bodyBuf.WriteString(line)
			continue
		}

		up := strings.ToUpper(trimmed)
		switch {
		case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
			writeLine("250-mock hello")
			writeLine("250 AUTH PLAIN")
		case strings.HasPrefix(up, "AUTH"):
			writeLine("235 auth ok")
		case strings.HasPrefix(up, "MAIL FROM"):
			cap.from = trimmed
			writeLine("250 ok")
		case strings.HasPrefix(up, "RCPT TO"):
			cap.rcpt = trimmed
			writeLine("250 ok")
		case up == "DATA":
			inData = true
			writeLine("354 end data with <CR><LF>.<CR><LF>")
		case up == "QUIT":
			writeLine("221 bye")
			s.once.Do(func() { s.done <- cap })
			return
		default:
			writeLine("250 ok")
		}
	}
}

func (s *smtpMockServer) wait(t *testing.T, timeout time.Duration) smtpCapture {
	t.Helper()
	select {
	case c := <-s.done:
		return c
	case <-time.After(timeout):
		if e, ok := s.lastErr.Load().(string); ok && e != "" {
			t.Fatalf("mock SMTP error: %s", e)
		}
		t.Fatal("mock SMTP: timed out waiting for message")
		return smtpCapture{}
	}
}

func (s *smtpMockServer) close() { _ = s.ln.Close() }

// injectEmailRunning builds an EmailBot with running=true injected.
// This skips Start() (which would dial a real IMAP TLS endpoint).
func injectEmailRunning(t *testing.T, smtpHost string, smtpPort int) *botemail.EmailBot {
	t.Helper()
	b := botemail.NewEmailBot(
		"127.0.0.1", 0, "imap-user@example.com", "imap-pwd",
		smtpHost, smtpPort, "bot@example.com", "smtp-pwd",
	)
	setPrivateField(t, b, "running", true)
	return b
}

// TestEmailBot_SendMessage_Success — SMTP happy path: email sent, headers + body asserted.
func TestEmailBot_SendMessage_Success(t *testing.T) {
	srv := startMockSMTP(t)
	defer srv.close()

	_, portStr, _ := net.SplitHostPort(srv.addr)
	var port int
	// Parse port from "127.0.0.1:NNNNN".
	if idx := strings.LastIndex(portStr, ":"); idx >= 0 {
		portStr = portStr[idx+1:]
	}
	if _, err := scanInt(portStr, &port); err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}

	b := injectEmailRunning(t, "127.0.0.1", port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := b.SendMessage(ctx, "alice@example.com", "greetings", "hello over smtp"); err != nil {
		t.Fatalf("SendMessage error = %v, want nil", err)
	}

	cap := srv.wait(t, 3*time.Second)
	if !strings.Contains(cap.from, "bot@example.com") {
		t.Errorf("MAIL FROM = %q, want contains bot@example.com", cap.from)
	}
	if !strings.Contains(cap.rcpt, "alice@example.com") {
		t.Errorf("RCPT TO = %q, want contains alice@example.com", cap.rcpt)
	}
	if !strings.Contains(cap.body, "hello over smtp") {
		t.Errorf("body missing 'hello over smtp':\n%s", cap.body)
	}
	if !strings.Contains(cap.body, "Subject: greetings\r\n") {
		t.Errorf("body missing Subject header:\n%s", cap.body)
	}
	if !strings.Contains(cap.body, "From: bot@example.com\r\n") {
		t.Errorf("body missing From header:\n%s", cap.body)
	}
	if got := srv.called.Load(); got != 1 {
		t.Errorf("called = %d, want 1", got)
	}
}

// TestEmailBot_SendMessage_APIErr — SMTP server returns 5xx, SendMessage returns user-facing error.
//
// Strategy: Use a custom mock SMTP that responds with 550 to RCPT TO.
// The email bot uses smtp.SendMail which will surface the SMTP error.
func TestEmailBot_SendMessage_APIErr(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	var called atomic.Int32
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		called.Add(1)
		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		writeLine := func(l string) {
			_, _ = w.WriteString(l + "\r\n")
			_ = w.Flush()
		}
		// Greet → expect EHLO → accept → expect MAIL → accept → expect RCPT → REJECT 550.
		writeLine("220 mock ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			up := strings.ToUpper(strings.TrimRight(line, "\r\n"))
			switch {
			case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
				writeLine("250-mock hello")
				writeLine("250 AUTH PLAIN")
			case strings.HasPrefix(up, "AUTH"):
				writeLine("235 auth ok")
			case strings.HasPrefix(up, "MAIL FROM"):
				writeLine("250 ok")
			case strings.HasPrefix(up, "RCPT TO"):
				writeLine("550 user unknown")
			default:
				writeLine("250 ok")
			}
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	if _, err := scanInt(portStr, &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}

	b := injectEmailRunning(t, "127.0.0.1", port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = b.SendMessage(ctx, "unknown@example.com", "s", "b")
	if err == nil {
		t.Fatal("SendMessage with SMTP 550 expected error, got nil")
	}
	if !strings.Contains(err.Error(), "暂时无法回复") {
		t.Errorf("err = %q, want user-facing prefix '暂时无法回复'", err.Error())
	}
	if got := called.Load(); got < 1 {
		t.Errorf("called = %d, want >=1", got)
	}
}

// TestEmailBot_SendMessage_AuthFail — SMTP server rejects AUTH.
//
// Strategy: Mock SMTP returns 535 Authentication credentials invalid for AUTH command.
// net/smtp.PlainAuth will then fail at the auth stage and SendMail returns an error.
func TestEmailBot_SendMessage_AuthFail(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	var called atomic.Int32
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		called.Add(1)
		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		writeLine := func(l string) {
			_, _ = w.WriteString(l + "\r\n")
			_ = w.Flush()
		}
		writeLine("220 mock ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			up := strings.ToUpper(strings.TrimRight(line, "\r\n"))
			switch {
			case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
				writeLine("250-mock hello")
				writeLine("250 AUTH PLAIN")
			case strings.HasPrefix(up, "AUTH"):
				writeLine("535 authentication credentials invalid")
			default:
				writeLine("250 ok")
			}
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	if _, err := scanInt(portStr, &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}

	b := injectEmailRunning(t, "127.0.0.1", port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = b.SendMessage(ctx, "alice@example.com", "s", "b")
	if err == nil {
		t.Fatal("SendMessage with SMTP 535 expected error, got nil")
	}
	if !strings.Contains(err.Error(), "暂时无法回复") {
		t.Errorf("err = %q, want user-facing prefix '暂时无法回复'", err.Error())
	}
	if got := called.Load(); got < 1 {
		t.Errorf("called = %d, want >=1", got)
	}
}

// scanInt parses a positive integer string into *out. Used for "127.0.0.1:NNNNN" → port int.
func scanInt(s string, out *int) (int, error) {
	n := 0
	if s == "" {
		return 0, ioEOF(s)
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, ioEOF(s)
		}
		n = n*10 + int(r-'0')
	}
	*out = n
	return len(s), nil
}

// ioEOF small sentinel for "bad number" without depending on io package directly.
type errBadNum string

func (e errBadNum) Error() string { return "bad number: " + string(e) }

func ioEOF(s string) error { return errBadNum(s) }

package mailer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testSMTPUsername = "user@sumopod.test"
	testSMTPPassword = "super-secret-smtp-password"
	testSMTPFrom     = "SWEG-AI <noreply@ai-sweg.my.id>"
)

var (
	testSMTPNow      = time.Date(2026, 10, 28, 15, 4, 5, 0, time.UTC)
	testMessageID    = regexp.MustCompile(`^<[0-9a-f]{32}@ai-sweg\.my\.id>$`)
	testSubjectASCII = "Tautan login SWEG-AI"
)

// fakeSMTPServer is a scripted in-process SMTP server. It speaks the subset of
// RFC 5321 that net/smtp needs: greeting, EHLO with AUTH PLAIN, AUTH, MAIL,
// RCPT, DATA, and QUIT. DATA lines are un-stuffed per RFC 5321.
type fakeSMTPServer struct {
	ln net.Listener

	mu       sync.Mutex
	authFail bool
	mailFrom string
	rcptTo   string
	authUser string
	authPass string
	data     []byte
	commands []string
}

type smtpSession struct {
	mailFrom string
	rcptTo   string
	authUser string
	authPass string
	data     []byte
	commands []string
}

func newFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake SMTP server: %v", err)
	}
	s := &fakeSMTPServer{ln: ln}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeSMTPServer) hostPort(t *testing.T) (string, string) {
	t.Helper()
	host, port, err := net.SplitHostPort(s.ln.Addr().String())
	if err != nil {
		t.Fatalf("split fake SMTP address: %v", err)
	}
	return host, port
}

func (s *fakeSMTPServer) rejectAuth() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authFail = true
}

func (s *fakeSMTPServer) session() smtpSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return smtpSession{
		mailFrom: s.mailFrom,
		rcptTo:   s.rcptTo,
		authUser: s.authUser,
		authPass: s.authPass,
		data:     append([]byte(nil), s.data...),
		commands: append([]string(nil), s.commands...),
	}
}

func (s *fakeSMTPServer) recordCommand(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, line)
}

func (s *fakeSMTPServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.handleConn(conn)
	}
}

func (s *fakeSMTPServer) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	reply := func(line string) bool {
		if _, err := writer.WriteString(line + "\r\n"); err != nil {
			return false
		}
		return writer.Flush() == nil
	}
	if !reply("220 fake.smtp.test ESMTP ready") {
		return
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.recordCommand(line)
		switch {
		case strings.HasPrefix(line, "EHLO "):
			if !reply("250-fake.smtp.test") || !reply("250 AUTH PLAIN") {
				return
			}
		case strings.HasPrefix(line, "HELO "):
			if !reply("250 fake.smtp.test") {
				return
			}
		case strings.HasPrefix(line, "AUTH PLAIN"):
			if !s.handleAuth(strings.TrimSpace(strings.TrimPrefix(line, "AUTH PLAIN")), reply) {
				return
			}
		case strings.HasPrefix(line, "MAIL FROM:"):
			s.mu.Lock()
			s.mailFrom = strings.TrimSpace(strings.TrimPrefix(line, "MAIL FROM:"))
			s.mu.Unlock()
			if !reply("250 2.1.0 sender ok") {
				return
			}
		case strings.HasPrefix(line, "RCPT TO:"):
			s.mu.Lock()
			s.rcptTo = strings.TrimSpace(strings.TrimPrefix(line, "RCPT TO:"))
			s.mu.Unlock()
			if !reply("250 2.1.5 recipient ok") {
				return
			}
		case line == "DATA":
			if !reply("354 end with <CRLF>.<CRLF>") {
				return
			}
			data, err := readSMTPData(reader)
			if err != nil {
				return
			}
			s.mu.Lock()
			s.data = data
			s.mu.Unlock()
			if !reply("250 2.0.0 queued") {
				return
			}
		case line == "QUIT":
			_ = reply("221 2.0.0 bye")
			return
		case line == "RSET":
			if !reply("250 2.0.0 reset") {
				return
			}
		default:
			if !reply("502 5.5.2 command not recognized") {
				return
			}
		}
	}
}

func (s *fakeSMTPServer) handleAuth(payload string, reply func(string) bool) bool {
	user, pass, err := decodePlainAuth(payload)
	if err != nil {
		return reply("501 5.5.2 malformed AUTH PLAIN")
	}
	s.mu.Lock()
	s.authUser = user
	s.authPass = pass
	reject := s.authFail
	s.mu.Unlock()
	if reject {
		return reply("535 5.7.8 credentials rejected by policy")
	}
	return reply("235 2.7.0 authentication successful")
}

// readSMTPData reads the DATA payload until the terminating line and undoes
// the dot-stuffing from RFC 5321. Returned bytes keep their CRLF endings.
func readSMTPData(reader *bufio.Reader) ([]byte, error) {
	var data bytes.Buffer
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == ".\r\n" || line == ".\n" {
			return data.Bytes(), nil
		}
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		data.WriteString(line)
	}
}

func decodePlainAuth(payload string) (string, string, error) {
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", "", err
	}
	fields := strings.Split(string(raw), "\x00")
	if len(fields) != 3 {
		return "", "", fmt.Errorf("AUTH PLAIN has %d fields, want 3", len(fields))
	}
	return fields[1], fields[2], nil
}

// newTestSMTP builds a production-configured SMTP client and swaps only the
// dial seam: plain TCP to the fake server, because it cannot speak TLS.
func newTestSMTP(t *testing.T, srv *fakeSMTPServer) *SMTP {
	t.Helper()
	return newTestSMTPFrom(t, srv, testSMTPFrom)
}

// newTestSMTPFrom is newTestSMTP with an explicit MAIL_FROM value.
func newTestSMTPFrom(t *testing.T, srv *fakeSMTPServer, from string) *SMTP {
	t.Helper()
	host, port := srv.hostPort(t)
	client := NewSMTP(host, port, testSMTPUsername, testSMTPPassword, from)
	client.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, addr)
	}
	client.now = func() time.Time { return testSMTPNow }
	return client
}

// newNoDialSMTP builds a client whose dial seam records an unexpected call, so
// validation tests prove no connection opens before a message is accepted.
func newNoDialSMTP(t *testing.T) (*SMTP, *bool) {
	t.Helper()
	client := NewSMTP("127.0.0.1", "465", testSMTPUsername, testSMTPPassword, testSMTPFrom)
	client.now = func() time.Time { return testSMTPNow }
	dialed := false
	client.dial = func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("unexpected dial")
	}
	return client, &dialed
}

func TestSMTPSendDeliversMultipartMessage(t *testing.T) {
	srv := newFakeSMTPServer(t)
	client := newTestSMTP(t, srv)
	text := "Link: https://ai-sweg.my.id/login/magic?token=secret-token\n\nThis link works once."
	html := "<p>Link: https://ai-sweg.my.id/login/magic?token=secret-token</p>"

	err := client.Send(context.Background(), Message{
		To:             "budi@test.id",
		Subject:        testSubjectASCII,
		Text:           text,
		HTML:           html,
		IdempotencyKey: "hash-123",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	session := srv.session()
	if session.mailFrom != "<noreply@ai-sweg.my.id>" {
		t.Fatalf("MAIL FROM = %q, want the bare envelope sender", session.mailFrom)
	}
	if session.rcptTo != "<budi@test.id>" {
		t.Fatalf("RCPT TO = %q, want the recipient", session.rcptTo)
	}
	if session.authUser != testSMTPUsername || session.authPass != testSMTPPassword {
		t.Fatalf("AUTH PLAIN decoded to (%q, %q), want the configured credentials", session.authUser, session.authPass)
	}

	raw := session.data
	if hasBareLF(raw) {
		t.Fatalf("DATA contains a bare LF, want CRLF line endings:\n%s", raw)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse sent message: %v", err)
	}
	for _, header := range []struct{ name, want string }{
		{"To", "budi@test.id"},
		{"Subject", testSubjectASCII},
		{"Date", "Wed, 28 Oct 2026 15:04:05 +0000"},
		{"MIME-Version", "1.0"},
	} {
		if got := msg.Header.Get(header.name); got != header.want {
			t.Fatalf("%s = %q, want %q", header.name, got, header.want)
		}
	}
	fromHeader := msg.Header.Get("From")
	fromAddr, err := mail.ParseAddress(fromHeader)
	if err != nil {
		t.Fatalf("parse From header %q: %v", fromHeader, err)
	}
	if fromAddr.Name != "SWEG-AI" || fromAddr.Address != "noreply@ai-sweg.my.id" {
		t.Fatalf("From = %q, want display name SWEG-AI and bare sender address", fromHeader)
	}
	if got := msg.Header.Get("Message-ID"); !testMessageID.MatchString(got) {
		t.Fatalf("Message-ID = %q, want <32 hex chars>@ai-sweg.my.id", got)
	}
	if got := msg.Header.Get("Reply-To"); got != "noreply@ai-sweg.my.id" {
		t.Fatalf("Reply-To = %q, want the bare sender address", got)
	}
	if got := msg.Header.Get("Auto-Submitted"); got != "auto-generated" {
		t.Fatalf("Auto-Submitted = %q, want auto-generated", got)
	}
	if got := msg.Header.Get("X-Auto-Response-Suppress"); got != "All" {
		t.Fatalf("X-Auto-Response-Suppress = %q, want All", got)
	}

	parts := readSentParts(t, msg)
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want a text and an HTML part", len(parts))
	}
	for _, want := range []struct {
		mediaType string
		body      string
	}{
		{"text/plain", text},
		{"text/html", html},
	} {
		part, ok := parts[want.mediaType]
		if !ok {
			t.Fatalf("missing %s part, got %v", want.mediaType, parts)
		}
		if part.encoding != "base64" {
			t.Fatalf("%s Content-Transfer-Encoding = %q, want base64", want.mediaType, part.encoding)
		}
		if !strings.Contains(part.contentType, "charset=UTF-8") {
			t.Fatalf("%s Content-Type = %q, want charset=UTF-8", want.mediaType, part.contentType)
		}
		if string(part.body) != want.body {
			t.Fatalf("%s body = %q, want %q", want.mediaType, part.body, want.body)
		}
	}
}

func TestSMTPSendEncodesNonASCIISubject(t *testing.T) {
	srv := newFakeSMTPServer(t)
	client := newTestSMTP(t, srv)
	subject := "Tautan masuk swegai: ログイン"

	err := client.Send(context.Background(), Message{
		To:      "budi@test.id",
		Subject: subject,
		Text:    "Use this link to sign in.",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	msg, err := mail.ReadMessage(bytes.NewReader(srv.session().data))
	if err != nil {
		t.Fatalf("parse sent message: %v", err)
	}
	encoded := msg.Header.Get("Subject")
	if !strings.Contains(encoded, "=?UTF-8?q?") {
		t.Fatalf("Subject = %q, want an RFC 2047 Q-encoded word", encoded)
	}
	decoded, err := new(mime.WordDecoder).DecodeHeader(encoded)
	if err != nil {
		t.Fatalf("decode Subject: %v", err)
	}
	if decoded != subject {
		t.Fatalf("Subject decodes to %q, want %q", decoded, subject)
	}
}

func TestSMTPMessageIDUsesSenderDomainNotSMTPHost(t *testing.T) {
	srv := newFakeSMTPServer(t)
	client := newTestSMTPFrom(t, srv, "SWEG-AI <Login@AI-Sweg.My.ID>")

	if err := client.Send(context.Background(), Message{
		To:      "budi@test.id",
		Subject: testSubjectASCII,
		Text:    "body",
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	got := mustReadMessage(t, srv.session().data).Header.Get("Message-ID")
	if !strings.HasSuffix(got, "@ai-sweg.my.id>") {
		t.Fatalf("Message-ID = %q, want the lowercased sender domain so DMARC and DKIM align", got)
	}
}

func TestSMTPFromHeaderEncodesNonASCIIDisplayName(t *testing.T) {
	srv := newFakeSMTPServer(t)
	client := newTestSMTPFrom(t, srv, "日本語 <noreply@ai-sweg.my.id>")

	if err := client.Send(context.Background(), Message{
		To:      "budi@test.id",
		Subject: testSubjectASCII,
		Text:    "body",
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	encoded := mustReadMessage(t, srv.session().data).Header.Get("From")
	if !strings.Contains(strings.ToLower(encoded), "=?utf-8?") {
		t.Fatalf("From = %q, want an RFC 2047 encoded display name", encoded)
	}
	decoded, err := new(mime.WordDecoder).DecodeHeader(encoded)
	if err != nil {
		t.Fatalf("decode From header: %v", err)
	}
	addr, err := mail.ParseAddress(decoded)
	if err != nil {
		t.Fatalf("parse decoded From %q: %v", decoded, err)
	}
	if addr.Name != "日本語" || addr.Address != "noreply@ai-sweg.my.id" {
		t.Fatalf("From = %q, want display name 日本語 and bare sender address", decoded)
	}
}

func TestSMTPSendUsesDistinctMessageIDs(t *testing.T) {
	srv := newFakeSMTPServer(t)
	client := newTestSMTP(t, srv)
	ids := make([]string, 0, 2)
	for range 2 {
		if err := client.Send(context.Background(), Message{
			To:      "budi@test.id",
			Subject: testSubjectASCII,
			Text:    "body",
		}); err != nil {
			t.Fatalf("Send() error = %v", err)
		}
		ids = append(ids, mustReadMessage(t, srv.session().data).Header.Get("Message-ID"))
	}
	if ids[0] == "" || ids[1] == "" {
		t.Fatalf("Message-IDs = %q, want two non-empty values", ids)
	}
	if ids[0] == ids[1] {
		t.Fatalf("both sends used Message-ID %q, want distinct IDs", ids[0])
	}
}

func TestSMTPSendPreservesDotPrefixedBodyLines(t *testing.T) {
	srv := newFakeSMTPServer(t)
	client := newTestSMTP(t, srv)
	text := "First line\n.leading dot line\n..two dots\nLast line"

	err := client.Send(context.Background(), Message{
		To:      "budi@test.id",
		Subject: testSubjectASCII,
		Text:    text,
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	session := srv.session()
	if hasBareLF(session.data) {
		t.Fatalf("DATA contains a bare LF, want CRLF line endings:\n%s", session.data)
	}
	for _, line := range strings.Split(string(session.data), "\r\n") {
		if line == "." {
			t.Fatal("DATA carries a bare terminator line, so the message was not dot-safe")
		}
	}
	textPart := readSentParts(t, mustReadMessage(t, session.data))["text/plain"]
	if string(textPart.body) != text {
		t.Fatalf("text body = %q, want the full dot-prefixed text %q", textPart.body, text)
	}
}

func TestSMTPSendOmitsEmptyParts(t *testing.T) {
	for name, msg := range map[string]Message{
		"text_only": {To: "budi@test.id", Subject: testSubjectASCII, Text: "plain body"},
		"html_only": {To: "budi@test.id", Subject: testSubjectASCII, HTML: "<p>html body</p>"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newFakeSMTPServer(t)
			client := newTestSMTP(t, srv)
			if err := client.Send(context.Background(), msg); err != nil {
				t.Fatalf("Send() error = %v", err)
			}
			if parts := readSentParts(t, mustReadMessage(t, srv.session().data)); len(parts) != 1 {
				t.Fatalf("parts = %d, want only the non-empty part", len(parts))
			}
		})
	}
}

func TestSMTPSendConnectionRefusedReturnsErrSendFailed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}
	_ = ln.Close()

	client := NewSMTP(host, port, testSMTPUsername, testSMTPPassword, testSMTPFrom)
	err = client.Send(context.Background(), Message{To: "budi@test.id", Subject: testSubjectASCII, Text: "body"})
	if !errors.Is(err, ErrSendFailed) {
		t.Fatalf("Send() error = %v, want ErrSendFailed", err)
	}
	if !strings.Contains(err.Error(), "dial") {
		t.Fatalf("error = %v, want the dial stage", err)
	}
}

func TestSMTPSendAuthFailureDoesNotLeakCredentials(t *testing.T) {
	srv := newFakeSMTPServer(t)
	srv.rejectAuth()
	client := newTestSMTP(t, srv)

	err := client.Send(context.Background(), Message{To: "budi@test.id", Subject: testSubjectASCII, Text: "body"})
	if !errors.Is(err, ErrSendFailed) {
		t.Fatalf("Send() error = %v, want ErrSendFailed", err)
	}
	if !strings.Contains(err.Error(), "auth") {
		t.Fatalf("error = %v, want the auth stage", err)
	}
	for _, secret := range []string{testSMTPUsername, testSMTPPassword, "credentials rejected by policy"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
	if session := srv.session(); session.mailFrom != "" {
		t.Fatalf("MAIL FROM = %q sent after auth failure", session.mailFrom)
	}
}

func TestSMTPSendRejectsHeaderInjectionBeforeDial(t *testing.T) {
	client, dialed := newNoDialSMTP(t)
	for name, msg := range map[string]Message{
		"subject_crlf": {To: "budi@test.id", Subject: "Login\r\nBcc: evil@test.id", Text: "body"},
		"subject_lf":   {To: "budi@test.id", Subject: "Login\nBcc: evil@test.id", Text: "body"},
		"to_crlf":      {To: "budi@test.id\r\nBcc: evil@test.id", Subject: testSubjectASCII, Text: "body"},
		"to_lf":        {To: "budi@test.id\nBcc: evil@test.id", Subject: testSubjectASCII, Text: "body"},
	} {
		err := client.Send(context.Background(), msg)
		if !errors.Is(err, ErrSendFailed) {
			t.Fatalf("%s: Send() error = %v, want ErrSendFailed", name, err)
		}
		if !strings.Contains(err.Error(), "invalid message") {
			t.Fatalf("%s: error = %v, want the invalid message stage", name, err)
		}
	}
	if *dialed {
		t.Fatal("Send dialed before rejecting a header-injected message")
	}
}

func TestSMTPSendRejectsInvalidMessageBeforeDial(t *testing.T) {
	client, dialed := newNoDialSMTP(t)
	for name, msg := range map[string]Message{
		"empty_to":      {Subject: testSubjectASCII, Text: "body"},
		"empty_subject": {To: "budi@test.id", Text: "body"},
		"empty_body":    {To: "budi@test.id", Subject: testSubjectASCII},
		"bad_address":   {To: "not an address", Subject: testSubjectASCII, Text: "body"},
	} {
		err := client.Send(context.Background(), msg)
		if !errors.Is(err, ErrSendFailed) {
			t.Fatalf("%s: Send() error = %v, want ErrSendFailed", name, err)
		}
		if !strings.Contains(err.Error(), "invalid message") {
			t.Fatalf("%s: error = %v, want the invalid message stage", name, err)
		}
	}
	if *dialed {
		t.Fatal("Send dialed before rejecting an invalid message")
	}
}

func TestSMTPSendHonorsCanceledContext(t *testing.T) {
	client := NewSMTP("127.0.0.1", "465", testSMTPUsername, testSMTPPassword, testSMTPFrom)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var dialCtxErr error
	client.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialCtxErr = ctx.Err()
		return nil, ctx.Err()
	}

	err := client.Send(ctx, Message{To: "budi@test.id", Subject: testSubjectASCII, Text: "body"})
	if !errors.Is(err, ErrSendFailed) {
		t.Fatalf("Send() error = %v, want ErrSendFailed", err)
	}
	if !strings.Contains(err.Error(), "dial") {
		t.Fatalf("error = %v, want the dial stage", err)
	}
	if !errors.Is(dialCtxErr, context.Canceled) {
		t.Fatalf("dial context error = %v, want context.Canceled", dialCtxErr)
	}
}

// TestSMTPSendDialsImplicitTLS proves the production dial seam starts a TLS
// handshake without any STARTTLS command, so port 465 keeps working.
func TestSMTPSendDialsImplicitTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}
	firstByte := make(chan byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		buf := make([]byte, 1)
		if _, err := io.ReadFull(conn, buf); err == nil {
			firstByte <- buf[0]
		}
	}()

	client := NewSMTP(host, port, testSMTPUsername, testSMTPPassword, testSMTPFrom)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err = client.Send(ctx, Message{To: "budi@test.id", Subject: testSubjectASCII, Text: "body"})
	if !errors.Is(err, ErrSendFailed) {
		t.Fatalf("Send() error = %v, want ErrSendFailed", err)
	}
	select {
	case b := <-firstByte:
		const tlsHandshakeRecord = 0x16
		if b != tlsHandshakeRecord {
			t.Fatalf("first byte = %#x, want a TLS handshake record %#x", b, tlsHandshakeRecord)
		}
	case <-time.After(time.Second):
		t.Fatal("dial sent no TLS handshake bytes")
	}
}

func TestNewSMTPPanicsOnInvalidConfig(t *testing.T) {
	type smtpArgs struct {
		host     string
		port     string
		username string
		password string
		from     string
	}
	valid := smtpArgs{
		host:     "smtp.sumopod.com",
		port:     "465",
		username: testSMTPUsername,
		password: testSMTPPassword,
		from:     testSMTPFrom,
	}
	for name, tc := range map[string]struct {
		mutate func(*smtpArgs)
		want   string
	}{
		"empty_host":     {func(a *smtpArgs) { a.host = "  " }, "mailer: empty SMTP_HOST"},
		"empty_username": {func(a *smtpArgs) { a.username = " " }, "mailer: empty SMTP_USERNAME"},
		"empty_password": {func(a *smtpArgs) { a.password = "" }, "mailer: empty SMTP_PASSWORD"},
		"empty_from":     {func(a *smtpArgs) { a.from = "" }, "mailer: empty MAIL_FROM"},
		"port_empty":     {func(a *smtpArgs) { a.port = "" }, "mailer: invalid SMTP_PORT"},
		"port_zero":      {func(a *smtpArgs) { a.port = "0" }, "mailer: invalid SMTP_PORT"},
		"port_too_high":  {func(a *smtpArgs) { a.port = "65536" }, "mailer: invalid SMTP_PORT"},
		"port_text":      {func(a *smtpArgs) { a.port = "smtp" }, "mailer: invalid SMTP_PORT"},
		"bad_from":       {func(a *smtpArgs) { a.from = "not an address" }, "mailer: invalid MAIL_FROM"},
		"from_crlf_comment": {func(a *smtpArgs) {
			a.from = "noreply@ai-sweg.my.id (x\r\nBcc: evil@test.id)"
		}, "mailer: invalid MAIL_FROM"},
		"from_trailing_crlf": {func(a *smtpArgs) {
			a.from = "noreply@ai-sweg.my.id\r\n"
		}, "mailer: invalid MAIL_FROM"},
	} {
		t.Run(name, func(t *testing.T) {
			args := valid
			tc.mutate(&args)
			expectPanic(t, tc.want, func() {
				NewSMTP(args.host, args.port, args.username, args.password, args.from)
			})
		})
	}
}

type sentPart struct {
	contentType string
	encoding    string
	body        []byte
}

func readSentParts(t *testing.T, msg *mail.Message) map[string]sentPart {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse Content-Type: %v", err)
	}
	if mediaType != "multipart/alternative" {
		t.Fatalf("Content-Type = %q, want multipart/alternative", mediaType)
	}
	reader := multipart.NewReader(msg.Body, params["boundary"])
	parts := make(map[string]sentPart)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return parts
		}
		if err != nil {
			t.Fatalf("read MIME part: %v", err)
		}
		partType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parse part Content-Type: %v", err)
		}
		encoded, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read part body: %v", err)
		}
		body, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(encoded), "\r\n", ""))
		if err != nil {
			t.Fatalf("decode part body: %v", err)
		}
		parts[partType] = sentPart{
			contentType: part.Header.Get("Content-Type"),
			encoding:    part.Header.Get("Content-Transfer-Encoding"),
			body:        body,
		}
	}
}

func mustReadMessage(t *testing.T, raw []byte) *mail.Message {
	t.Helper()
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse sent message: %v", err)
	}
	return msg
}

func hasBareLF(raw []byte) bool {
	for i, b := range raw {
		if b == '\n' && (i == 0 || raw[i-1] != '\r') {
			return true
		}
	}
	return false
}

func expectPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		got := recover()
		if got == nil {
			t.Fatalf("expected panic %q", want)
		}
		if got != want {
			t.Fatalf("panic = %v, want %q", got, want)
		}
	}()
	fn()
}

func TestLogMailerWritesToLogger(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	m := NewLogMailer(log)

	err := m.Send(context.Background(), Message{
		To:             "budi@test.id",
		Subject:        "Tautan login SWEG-AI",
		Text:           "https://ai-sweg.my.id/login/magic?token=abc",
		IdempotencyKey: "hash-abc",
	})
	if err != nil {
		t.Fatalf("LogMailer.Send() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "budi@test.id") || !strings.Contains(out, "token=abc") {
		t.Fatalf("log output = %q, want recipient and link", out)
	}
}

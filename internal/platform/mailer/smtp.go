package mailer

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

const (
	sendTimeout     = 10 * time.Second
	messageIDBytes  = 16
	base64LineWidth = 76
	smtpPortMin     = 1
	smtpPortMax     = 65535
)

// SMTP sends transactional email over an implicit TLS SMTP session.
type SMTP struct {
	host         string
	port         string
	username     string
	password     string
	fromHeader   string
	envelopeFrom string
	idDomain     string
	timeout      time.Duration

	// dial and now are test seams. NewSMTP sets the TLS dialer and time.Now;
	// same-package tests swap in a plain-TCP server and a fixed clock.
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
	now  func() time.Time
}

// NewSMTP wires the live implicit TLS SMTP session used on port 465.
// Invalid settings panic at boot.
func NewSMTP(host, port, username, password, from string) *SMTP {
	// ParseAddress accepts CRLF inside a comment, so reject line breaks before
	// any trimming or parsing. The From header is rendered through
	// Address.String, which encodes non-ASCII display names and neutralizes any
	// remaining header trickery.
	if strings.ContainsAny(from, "\r\n") {
		panic("mailer: invalid MAIL_FROM")
	}
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	username = strings.TrimSpace(username)
	from = strings.TrimSpace(from)
	if host == "" {
		panic("mailer: empty SMTP_HOST")
	}
	if username == "" {
		panic("mailer: empty SMTP_USERNAME")
	}
	if password == "" {
		panic("mailer: empty SMTP_PASSWORD")
	}
	if from == "" {
		panic("mailer: empty MAIL_FROM")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < smtpPortMin || portNumber > smtpPortMax {
		panic("mailer: invalid SMTP_PORT")
	}
	sender, err := mail.ParseAddress(from)
	if err != nil {
		panic("mailer: invalid MAIL_FROM")
	}
	tlsDialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: sendTimeout},
		Config:    &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12},
	}
	return &SMTP{
		host:         host,
		port:         port,
		username:     username,
		password:     password,
		fromHeader:   sender.String(),
		envelopeFrom: sender.Address,
		idDomain:     addressDomain(sender.Address),
		timeout:      sendTimeout,
		dial:         tlsDialer.DialContext,
		now:          time.Now,
	}
}

// addressDomain returns the domain part of a bare address, falling back to
// "localhost" so Message-ID stays syntactically valid.
func addressDomain(address string) string {
	if _, domain, ok := strings.Cut(address, "@"); ok {
		if domain = strings.ToLower(strings.TrimSpace(domain)); domain != "" {
			return domain
		}
	}
	return "localhost"
}

// Send delivers one multipart MIME email over SMTP. Every failure wraps
// ErrSendFailed and names only the stage. IdempotencyKey is ignored: SMTP has
// no idempotent replay, LogMailer still logs the key.
func (s *SMTP) Send(ctx context.Context, msg Message) error {
	to, subject, err := normalizeMessage(msg)
	if err != nil {
		return err
	}
	raw, err := s.buildMessage(to, subject, msg)
	if err != nil {
		return fmt.Errorf("%w: invalid message", ErrSendFailed)
	}

	sessionCtx := ctx
	if s.timeout > 0 {
		var cancel context.CancelFunc
		sessionCtx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}
	conn, err := s.dial(sessionCtx, "tcp", net.JoinHostPort(s.host, s.port))
	if err != nil {
		return fmt.Errorf("%w: dial", ErrSendFailed)
	}
	defer func() { _ = conn.Close() }()
	if deadline, ok := sessionCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("%w: dial", ErrSendFailed)
	}
	if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
		return fmt.Errorf("%w: auth", ErrSendFailed)
	}
	if err := client.Mail(s.envelopeFrom); err != nil {
		return fmt.Errorf("%w: mail from", ErrSendFailed)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("%w: rcpt", ErrSendFailed)
	}
	body, err := client.Data()
	if err != nil {
		return fmt.Errorf("%w: data", ErrSendFailed)
	}
	if _, err := body.Write(raw); err != nil {
		_ = body.Close()
		return fmt.Errorf("%w: data", ErrSendFailed)
	}
	if err := body.Close(); err != nil {
		return fmt.Errorf("%w: data", ErrSendFailed)
	}
	_ = client.Quit()
	return nil
}

// normalizeMessage validates the message and returns the bare recipient and
// the trimmed subject. Headers are checked for CRLF before any dial.
func normalizeMessage(msg Message) (*mail.Address, string, error) {
	invalid := fmt.Errorf("%w: invalid message", ErrSendFailed)
	if strings.ContainsAny(msg.To, "\r\n") || strings.ContainsAny(msg.Subject, "\r\n") {
		return nil, "", invalid
	}
	to := strings.TrimSpace(msg.To)
	subject := strings.TrimSpace(msg.Subject)
	if to == "" || subject == "" || (msg.Text == "" && msg.HTML == "") {
		return nil, "", invalid
	}
	address, err := mail.ParseAddress(to)
	if err != nil {
		return nil, "", invalid
	}
	return address, subject, nil
}

// buildMessage renders the RFC 5322 message with CRLF line endings. Both parts
// are base64 so the DATA phase never needs manual dot-stuffing.
func (s *SMTP) buildMessage(to *mail.Address, subject string, msg Message) ([]byte, error) {
	messageID, err := randomHex(messageIDBytes)
	if err != nil {
		return nil, err
	}
	boundary, err := randomHex(messageIDBytes)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.Grow(len(msg.Text) + len(msg.HTML) + 512)
	fmt.Fprintf(&b, "From: %s\r\n", s.fromHeader)
	fmt.Fprintf(&b, "To: %s\r\n", to.Address)
	fmt.Fprintf(&b, "Reply-To: %s\r\n", s.envelopeFrom)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", s.now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", messageID, s.idDomain)
	// Machine-generated mail: tell filters and vacation autoresponders that no
	// human reads replies, which keeps transactional mail out of bulk heuristics.
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	b.WriteString("X-Auto-Response-Suppress: All\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n", boundary)
	b.WriteString("\r\n")

	if msg.Text != "" {
		writeMIMEPart(&b, boundary, "text/plain", msg.Text)
	}
	if msg.HTML != "" {
		writeMIMEPart(&b, boundary, "text/html", msg.HTML)
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return []byte(b.String()), nil
}

func writeMIMEPart(b *strings.Builder, boundary, contentType, body string) {
	fmt.Fprintf(b, "--%s\r\n", boundary)
	fmt.Fprintf(b, "Content-Type: %s; charset=UTF-8\r\n", contentType)
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("\r\n")
	writeBase64Body(b, body)
}

func writeBase64Body(b *strings.Builder, body string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	for len(encoded) > base64LineWidth {
		b.WriteString(encoded[:base64LineWidth])
		b.WriteString("\r\n")
		encoded = encoded[base64LineWidth:]
	}
	b.WriteString(encoded)
	b.WriteString("\r\n")
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

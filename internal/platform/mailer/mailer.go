package mailer

import (
	"context"
	"errors"
	"log/slog"
	"strings"
)

// ErrSendFailed signals a failed email delivery from the mail provider.
var ErrSendFailed = errors.New("mailer: send failed")

// Message is one outgoing transactional email.
type Message struct {
	To             string
	Subject        string
	Text           string
	HTML           string
	IdempotencyKey string
}

// Mailer is the outbound email port.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// LogMailer writes outgoing emails to the structured logger instead of calling
// an external API. Use in local dev and tests when JETEMAIL_API_KEY is empty.
type LogMailer struct {
	log *slog.Logger
}

// NewLogMailer builds a LogMailer. Nil log falls back to slog.Default.
func NewLogMailer(log *slog.Logger) *LogMailer {
	if log == nil {
		log = slog.Default()
	}
	return &LogMailer{log: log}
}

// Send implements Mailer by logging the recipient, subject, and text body.
func (m *LogMailer) Send(_ context.Context, msg Message) error {
	if strings.TrimSpace(msg.To) == "" || strings.TrimSpace(msg.Subject) == "" {
		return ErrSendFailed
	}
	m.log.Info("mailer: log mail",
		"to", msg.To,
		"subject", msg.Subject,
		"idempotency_key", msg.IdempotencyKey,
		"text", msg.Text,
	)
	return nil
}

package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultJetEmailURL = "https://api.jetemail.com/email"
	sendTimeout        = 10 * time.Second
	maxResponseBytes   = 4 << 10
)

// JetEmail sends transactional emails through the JetEmail REST API.
type JetEmail struct {
	apiKey   string
	from     string
	endpoint string
	client   *http.Client
}

// NewJetEmail wires the live JetEmail endpoint. Empty credentials panic at boot.
func NewJetEmail(apiKey, from string) *JetEmail {
	return NewJetEmailWithClient(apiKey, from, defaultJetEmailURL, nil)
}

// NewJetEmailWithClient wires JetEmail with an explicit endpoint and HTTP client.
// Tests point endpoint at an httptest.Server; production uses NewJetEmail.
func NewJetEmailWithClient(apiKey, from, endpoint string, client *http.Client) *JetEmail {
	apiKey = strings.TrimSpace(apiKey)
	from = strings.TrimSpace(from)
	endpoint = strings.TrimSpace(endpoint)
	if apiKey == "" {
		panic("mailer: empty JETEMAIL_API_KEY")
	}
	if from == "" {
		panic("mailer: empty MAIL_FROM")
	}
	if endpoint == "" {
		endpoint = defaultJetEmailURL
	}
	if client == nil {
		client = &http.Client{Timeout: sendTimeout}
	}
	return &JetEmail{
		apiKey:   apiKey,
		from:     from,
		endpoint: endpoint,
		client:   client,
	}
}

type jetEmailPayload struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text,omitempty"`
	HTML    string `json:"html,omitempty"`
}

// Send posts one transactional email to JetEmail. Foreign errors and HTTP
// status codes are translated into ErrSendFailed without echoing message bodies
// or bearer keys.
func (j *JetEmail) Send(ctx context.Context, msg Message) error {
	to := strings.TrimSpace(msg.To)
	subject := strings.TrimSpace(msg.Subject)
	if to == "" || subject == "" || (msg.Text == "" && msg.HTML == "") {
		return ErrSendFailed
	}

	rawBody, err := json.Marshal(jetEmailPayload{
		From:    j.from,
		To:      to,
		Subject: subject,
		Text:    msg.Text,
		HTML:    msg.HTML,
	})
	if err != nil {
		return fmt.Errorf("%w: encode payload", ErrSendFailed)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return fmt.Errorf("%w: build request", ErrSendFailed)
	}
	req.Header.Set("Authorization", "Bearer "+j.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(msg.IdempotencyKey); key != "" {
		req.Header.Set("Idempotency-Key", key)
	}

	resp, err := j.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: transport failure", ErrSendFailed)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%w: status %d", ErrSendFailed, resp.StatusCode)
	}
	return nil
}

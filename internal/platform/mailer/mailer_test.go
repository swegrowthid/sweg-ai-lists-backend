package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJetEmailSendPostsExpectedHeadersAndBody(t *testing.T) {
	var (
		gotMethod      string
		gotAuth        string
		gotContentType string
		gotIdempotency string
		gotBody        jetEmailPayload
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotIdempotency = r.Header.Get("Idempotency-Key")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_123"}`))
	}))
	defer srv.Close()

	client := NewJetEmailWithClient("transactional_test_key", "SWEG-AI <noreply@ai-sweg.my.id>", srv.URL, srv.Client())
	err := client.Send(context.Background(), Message{
		To:             "budi@test.id",
		Subject:        "Tautan login SWEG-AI",
		Text:           "Link: https://ai-sweg.my.id/login/magic?token=secret-token",
		HTML:           "<p>Link</p>",
		IdempotencyKey: "hash-123",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotAuth != "Bearer transactional_test_key" {
		t.Fatalf("Authorization = %q, want Bearer key", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotIdempotency != "hash-123" {
		t.Fatalf("Idempotency-Key = %q, want hash-123", gotIdempotency)
	}
	if gotBody.From != "SWEG-AI <noreply@ai-sweg.my.id>" || gotBody.To != "budi@test.id" || gotBody.Subject != "Tautan login SWEG-AI" {
		t.Fatalf("payload = %+v, unexpected envelope fields", gotBody)
	}
	if !strings.Contains(gotBody.Text, "secret-token") || gotBody.HTML != "<p>Link</p>" {
		t.Fatalf("payload body = %+v, missing text or html", gotBody)
	}
}

func TestJetEmailSendErrorDoesNotLeakSecretOrToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"upstream failed"}`, http.StatusBadGateway)
	}))
	defer srv.Close()

	apiKey := "transactional_super_secret"
	rawToken := "raw_magic_token_value_that_must_not_leak"
	client := NewJetEmailWithClient(apiKey, "SWEG-AI <noreply@ai-sweg.my.id>", srv.URL, srv.Client())
	err := client.Send(context.Background(), Message{
		To:             "budi@test.id",
		Subject:        "Tautan login SWEG-AI",
		Text:           "https://ai-sweg.my.id/login/magic?token=" + rawToken,
		IdempotencyKey: "hash-123",
	})
	if !errors.Is(err, ErrSendFailed) {
		t.Fatalf("Send() error = %v, want ErrSendFailed", err)
	}
	if strings.Contains(err.Error(), apiKey) || strings.Contains(err.Error(), rawToken) {
		t.Fatalf("error leaked secret or token: %v", err)
	}
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

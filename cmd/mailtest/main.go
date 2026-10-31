// Command mailtest sends one real sign-in email with the production template.
//
// It uses the same renderer and SMTP mailer as POST /auth/magic-link, so the
// delivered message shows exactly what a user receives. The code and link are
// random samples: they do not log anyone in. Use this to check formatting and
// inbox placement, not to authenticate.
//
//	go run ./cmd/mailtest -to michaellapandio04@gmail.com
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/auth"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/config"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/mailer"
)

func main() {
	to := flag.String("to", "michaellapandio04@gmail.com", "recipient address")
	flag.Parse()

	if !strings.Contains(*to, "@") {
		log.Fatalf("mailtest: invalid recipient %q", *to)
	}

	cfg := config.Load()
	if strings.TrimSpace(cfg.SMTPHost) == "" {
		log.Fatal("mailtest: SMTP_HOST is empty - fill .env before sending")
	}
	if strings.TrimSpace(cfg.MailFrom) == "" {
		log.Fatal("mailtest: MAIL_FROM is empty - fill .env before sending")
	}

	baseURL := strings.TrimSpace(cfg.MagicLinkBaseURL)
	if baseURL == "" {
		baseURL = auth.DefaultMagicLinkBaseURL
	}
	ttl := cfg.MagicLinkTTL
	if ttl <= 0 {
		ttl = auth.DefaultMagicLinkTTL
	}
	minutes := int(ttl.Minutes())
	if minutes < 1 {
		minutes = 1
	}

	code, err := sampleCode()
	if err != nil {
		log.Fatalf("mailtest: generate code: %v", err)
	}
	token, err := sampleToken()
	if err != nil {
		log.Fatalf("mailtest: generate token: %v", err)
	}
	link, err := sampleLink(baseURL, token)
	if err != nil {
		log.Fatalf("mailtest: build link: %v", err)
	}

	subject, text, html := auth.MagicMailPreview(link, code, minutes)
	mail := mailer.NewSMTP(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.MailFrom)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Print to stderr so stdout holds only the outcome and sample values.
	fmt.Fprintf(os.Stderr, "sending to %s via %s:%s\n", *to, cfg.SMTPHost, cfg.SMTPPort)
	fmt.Fprintf(os.Stderr, "from    %s\n", cfg.MailFrom)
	fmt.Fprintf(os.Stderr, "subject %s\n", subject)
	fmt.Fprintf(os.Stderr, "code    %s\n", code)
	fmt.Fprintf(os.Stderr, "link    %s\n", link)

	if err := mail.Send(ctx, mailer.Message{
		To:      *to,
		Subject: subject,
		Text:    text,
		HTML:    html,
	}); err != nil {
		log.Fatalf("mailtest: send failed: %v", err)
	}
	fmt.Printf("sent to %s\n", *to)
}

// sampleCode returns a grouped 8-digit code, matching the real email shape.
func sampleCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(100_000_000))
	if err != nil {
		return "", err
	}
	digits := fmt.Sprintf("%08d", n.Int64())
	return digits[:4] + " " + digits[4:], nil
}

// sampleToken returns 32 random bytes as unpadded base64url, the real width.
func sampleToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// sampleLink appends the token as the query parameter the frontend reads.
func sampleLink(baseURL, token string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

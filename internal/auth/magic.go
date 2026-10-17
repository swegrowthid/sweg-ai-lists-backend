package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/mailer"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/user"
)

const (
	// DefaultMagicLinkTTL is the lifetime of one magic link when env is unset.
	DefaultMagicLinkTTL = 15 * time.Minute

	// DefaultMagicLinkBaseURL is the local frontend confirmation page used outside prod.
	DefaultMagicLinkBaseURL = "http://localhost:4321/login/magic"

	magicLinkRateWindow = time.Hour
	magicLinkRateLimit  = 5
	magicLinkStaleAge   = 24 * time.Hour
	magicTokenBytes     = 32
	magicMailSubject    = "Tautan login SWEG-AI"
)

var (
	// ErrMagicLinkInvalid signals an unknown, expired, or already-used magic link.
	// All three cases share one error so HTTP answers one generic 401 message.
	ErrMagicLinkInvalid = errors.New("auth: invalid or expired magic link")

	// ErrMagicRateLimited signals more than 5 magic link requests for one email in 1 hour.
	ErrMagicRateLimited = errors.New("auth: too many magic link requests")

	// ErrMagicEmailInvalid signals a malformed email in the magic link request.
	ErrMagicEmailInvalid = errors.New("auth: invalid email")
)

// MagicLinkRecord is the server-side state of one issued magic link.
// Only the SHA-256 hex digest of the token is persisted.
type MagicLinkRecord struct {
	TokenHash string
	UserID    string
	Email     string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// MagicStore is the persistence port for single-use magic links.
type MagicStore interface {
	// Save persists a new magic link and opportunistically prunes stale rows.
	Save(ctx context.Context, rec MagicLinkRecord) error

	// Consume atomically marks an unused, unexpired token as used and returns its row.
	// Unknown, expired, and already-used hashes all return ErrMagicLinkInvalid.
	Consume(ctx context.Context, tokenHash string, now time.Time) (MagicLinkRecord, error)

	// CountRecent counts magic link rows created for email after since.
	CountRecent(ctx context.Context, email string, since time.Time) (int, error)
}

// EmailFinder is the user lookup port Magic needs. user.Service and user.Store satisfy it.
type EmailFinder interface {
	FindByEmail(ctx context.Context, email string) (user.User, error)
}

// Magic owns the passwordless email login flow: request a link and exchange a
// single-use token for a standard JWT + refresh token pair.
type Magic struct {
	users   EmailFinder
	store   MagicStore
	tokens  *Tokens
	refresh RefreshStore
	mailer  mailer.Mailer
	baseURL string
	ttl     time.Duration
	now     func() time.Time
}

// NewMagic wires the magic link service. Nil dependencies or invalid settings panic at boot.
func NewMagic(
	users EmailFinder,
	store MagicStore,
	tokens *Tokens,
	refresh RefreshStore,
	m mailer.Mailer,
	baseURL string,
	ttl time.Duration,
) *Magic {
	if users == nil || store == nil || tokens == nil || refresh == nil || m == nil {
		panic("auth: nil Magic dependency")
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		panic("auth: empty magic link base URL")
	}
	if ttl <= 0 {
		panic("auth: non-positive magic link TTL")
	}
	return &Magic{
		users:   users,
		store:   store,
		tokens:  tokens,
		refresh: refresh,
		mailer:  m,
		baseURL: baseURL,
		ttl:     ttl,
		now:     time.Now,
	}
}

// Request validates the email, enforces the per-email hourly rate limit, and
// sends a single-use login link when the email belongs to a registered user.
// Unregistered emails return nil without sending mail so callers cannot
// enumerate accounts.
func (m *Magic) Request(ctx context.Context, rawEmail string) error {
	email, ok := normalizeEmail(rawEmail)
	if !ok {
		return ErrMagicEmailInvalid
	}

	now := m.now().UTC()
	recent, err := m.store.CountRecent(ctx, email, now.Add(-magicLinkRateWindow))
	if err != nil {
		return err
	}
	if recent >= magicLinkRateLimit {
		return ErrMagicRateLimited
	}

	u, err := m.users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return nil
		}
		return err
	}

	rawToken, tokenHash, err := newMagicToken()
	if err != nil {
		return err
	}

	rec := MagicLinkRecord{
		TokenHash: tokenHash,
		UserID:    u.ID,
		Email:     u.Email,
		ExpiresAt: now.Add(m.ttl),
		CreatedAt: now,
	}
	if err := m.store.Save(ctx, rec); err != nil {
		return err
	}

	link := buildMagicLinkURL(m.baseURL, rawToken)
	minutes := int(m.ttl.Minutes())
	if minutes < 1 {
		minutes = 1
	}
	textBody := fmt.Sprintf(
		"Gunakan tautan berikut untuk masuk ke SWEG-AI:\n\n%s\n\nTautan ini berlaku %d menit dan satu kali pakai.\nJika Anda tidak meminta tautan ini, abaikan email ini.",
		link, minutes,
	)
	htmlBody := fmt.Sprintf(
		"<p>Gunakan tautan berikut untuk masuk ke SWEG-AI:</p><p><a href=%q>Masuk ke SWEG-AI</a></p><p>%s</p><p>Tautan ini berlaku %d menit dan satu kali pakai.</p><p>Jika Anda tidak meminta tautan ini, abaikan email ini.</p>",
		link, html.EscapeString(link), minutes,
	)

	return m.mailer.Send(ctx, mailer.Message{
		To:             u.Email,
		Subject:        magicMailSubject,
		Text:           textBody,
		HTML:           htmlBody,
		IdempotencyKey: tokenHash,
	})
}

// Verify hashes the presented token, atomically consumes its row, and issues a
// fresh access + refresh token pair through the same path as password login.
func (m *Magic) Verify(ctx context.Context, rawToken string) (Pair, error) {
	token := strings.TrimSpace(rawToken)
	if token == "" || strings.ContainsRune(token, '\x00') {
		return Pair{}, ErrMagicLinkInvalid
	}

	tokenHash := hashMagicToken(token)
	rec, err := m.store.Consume(ctx, tokenHash, m.now().UTC())
	if err != nil {
		return Pair{}, err
	}

	u, err := m.users.FindByEmail(ctx, rec.Email)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return Pair{}, ErrMagicLinkInvalid
		}
		return Pair{}, err
	}

	pair, refreshRec, err := m.tokens.Issue(u.ID, u.Username)
	if err != nil {
		return Pair{}, err
	}
	if err := m.refresh.Save(ctx, refreshRec); err != nil {
		return Pair{}, err
	}
	return pair, nil
}

func newMagicToken() (raw string, hash string, err error) {
	var buf [magicTokenBytes]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", "", fmt.Errorf("auth: generate magic token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(buf[:])
	return raw, hashMagicToken(raw), nil
}

func hashMagicToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func buildMagicLinkURL(baseURL, token string) string {
	sep := "?"
	if strings.Contains(baseURL, "?") {
		sep = "&"
	}
	return baseURL + sep + "token=" + token
}

func normalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || strings.ContainsRune(email, '\x00') || strings.ContainsAny(email, " \t\r\n") {
		return "", false
	}
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 || strings.IndexByte(email[at+1:], '@') >= 0 {
		return "", false
	}
	return email, true
}

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/mailer"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/user"
)

const (
	// DefaultMagicLinkTTL is the lifetime of one magic link when env is unset.
	DefaultMagicLinkTTL = 15 * time.Minute

	// DefaultMagicLinkBaseURL is the local frontend confirmation page used outside prod.
	DefaultMagicLinkBaseURL = "http://localhost:4321/login/magic"

	magicLinkRateWindow  = time.Hour
	magicLinkRateLimit   = 5
	magicLinkStaleAge    = 24 * time.Hour
	magicTokenBytes      = 32
	magicCodeDigits      = 8
	magicCodeMaxAttempts = 5
	magicCodeSpace       = 100_000_000
	magicMailSubject     = "swegai sign-in code"
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
// Only the SHA-256 hex digests of the token and code are persisted.
type MagicLinkRecord struct {
	TokenHash    string
	CodeHash     string // SHA-256 hex of the 8-digit code
	CodeAttempts int
	UserID       string
	Email        string
	ExpiresAt    time.Time
	UsedAt       *time.Time
	CreatedAt    time.Time
}

// MagicStore is the persistence port for single-use magic links.
type MagicStore interface {
	// Save persists a new magic link and opportunistically prunes stale rows.
	Save(ctx context.Context, rec MagicLinkRecord) error

	// Consume atomically marks an unused, unexpired token as used and returns its row.
	// Unknown, expired, and already-used hashes all return ErrMagicLinkInvalid.
	Consume(ctx context.Context, tokenHash string, now time.Time) (MagicLinkRecord, error)

	// ConsumeCode atomically marks the active magic link for email as used when
	// the presented code hash matches, the row is unexpired, and fewer than
	// magicCodeMaxAttempts failures were recorded. On any miss it increments
	// code_attempts on the email's active row(s) and returns ErrMagicLinkInvalid.
	ConsumeCode(ctx context.Context, email, codeHash string, now time.Time) (MagicLinkRecord, error)

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
	rawCode, codeHash, err := newMagicCode()
	if err != nil {
		return err
	}

	rec := MagicLinkRecord{
		TokenHash: tokenHash,
		CodeHash:  codeHash,
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

	code := displayMagicCode(rawCode)
	return m.mailer.Send(ctx, mailer.Message{
		To:             u.Email,
		Subject:        magicMailSubject,
		Text:           magicMailText(link, code, minutes),
		HTML:           magicMailHTML(link, code, minutes),
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
	return m.issuePair(ctx, rec.Email)
}

// VerifyCode exchanges the 8-digit code from the same email for a token pair.
// The store enforces expiry, single use, and the attempt cap atomically.
func (m *Magic) VerifyCode(ctx context.Context, rawEmail, rawCode string) (Pair, error) {
	email, ok := normalizeEmail(rawEmail)
	if !ok {
		return Pair{}, ErrMagicLinkInvalid
	}
	code := normalizeMagicCode(rawCode)
	if code == "" {
		return Pair{}, ErrMagicLinkInvalid
	}

	rec, err := m.store.ConsumeCode(ctx, email, hashMagicToken(code), m.now().UTC())
	if err != nil {
		return Pair{}, err
	}
	return m.issuePair(ctx, rec.Email)
}

// issuePair maps a consumed magic link row to a fresh access + refresh pair.
func (m *Magic) issuePair(ctx context.Context, email string) (Pair, error) {
	u, err := m.users.FindByEmail(ctx, email)
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

// TTLSeconds exposes the magic link lifetime for API responses.
func (m *Magic) TTLSeconds() int {
	return int(m.ttl.Seconds())
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

func newMagicCode() (raw string, hash string, err error) {
	n, err := rand.Int(rand.Reader, big.NewInt(magicCodeSpace))
	if err != nil {
		return "", "", fmt.Errorf("auth: generate magic code: %w", err)
	}
	raw = fmt.Sprintf("%08d", n.Int64())
	return raw, hashMagicToken(raw), nil
}

// normalizeMagicCode strips all whitespace and dashes, then requires exactly
// magicCodeDigits ASCII digits. Users may paste "1234-5678" or "1234 5678".
func normalizeMagicCode(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		if unicode.IsSpace(r) || r == '-' {
			continue
		}
		b.WriteRune(r)
	}
	code := b.String()
	if len(code) != magicCodeDigits {
		return ""
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return ""
		}
	}
	return code
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

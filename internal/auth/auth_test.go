package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/swegrowthid/sweg-ai-lists-backend/internal/platform/mailer"
	"github.com/swegrowthid/sweg-ai-lists-backend/internal/user"
)

const testSecret = "test-secret-for-auth-tests"

// newTestMux builds the same graph as app.New with memory stores (swap R).
func newTestMux() *http.ServeMux {
	store := user.NewMemoryStore()
	userSvc := user.NewService(store)
	tokens := NewTokens(testSecret, 24*time.Hour, 24*time.Hour)
	mux := http.NewServeMux()
	user.NewHandler(userSvc, nil).RegisterRoutes(mux, NewMiddleware(tokens).RequireAuth)
	NewHandler(NewService(userSvc, tokens, NewMemoryRefreshStore()), nil).RegisterRoutes(mux)
	return mux
}

func doJSON(t *testing.T, mux *http.ServeMux, method, path, body, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	return res
}

func login(t *testing.T, mux *http.ServeMux, identifier, password string) Pair {
	t.Helper()
	res := doJSON(t, mux, http.MethodPost, "/auth/login",
		`{"identifier":"`+identifier+`","password":"`+password+`"}`, "")
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var pair Pair
	if err := json.Unmarshal(res.Body.Bytes(), &pair); err != nil {
		t.Fatalf("decode token pair: %v", err)
	}
	return pair
}

func TestLoginReturnsTokensWithoutPassword(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}

	res = doJSON(t, mux, http.MethodPost, "/auth/login",
		`{"identifier":"budi","password":"password"}`, "")
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["access_token"] == "" || body["refresh_token"] == "" {
		t.Fatal("login response misses tokens")
	}
	if body["token_type"] != "Bearer" {
		t.Fatalf("token_type = %v, want Bearer", body["token_type"])
	}
	if _, ok := body["password"]; ok {
		t.Fatal("response exposed password")
	}
	if _, ok := body["password_hash"]; ok {
		t.Fatal("response exposed password_hash")
	}
}

func TestLoginAcceptsEmailCaseInsensitive(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}
	pair := login(t, mux, "BUDI@Example.COM", "password")
	if pair.AccessToken == "" {
		t.Fatal("login by email returned no access token")
	}
}

func TestLoginWrongPasswordAndUnknownUserShareMessage(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}

	for _, body := range []string{
		`{"identifier":"budi","password":"wrong-password"}`,
		`{"identifier":"nobody","password":"password"}`,
	} {
		res := doJSON(t, mux, http.MethodPost, "/auth/login", body, "")
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("login status = %d, want %d; body = %s", res.Code, http.StatusUnauthorized, res.Body.String())
		}
		if got := strings.TrimSpace(res.Body.String()); got != "invalid credentials" {
			t.Fatalf("login message = %q, want generic message", got)
		}
	}
}

func TestProtectedUsersRejectsMissingAndGarbageTokens(t *testing.T) {
	mux := newTestMux()

	res := doJSON(t, mux, http.MethodGet, "/users", "", "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("GET /users without token status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
	if res.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("401 misses WWW-Authenticate header")
	}

	res = doJSON(t, mux, http.MethodGet, "/users", "", "not-a-token")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("GET /users with garbage token status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestProtectedUsersAcceptsAccessToken(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}
	pair := login(t, mux, "budi", "password")

	res = doJSON(t, mux, http.MethodGet, "/users", "", pair.AccessToken)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /users with token status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
}

func TestExpiredAccessTokenRejected(t *testing.T) {
	tokens := NewTokens(testSecret, 15*time.Minute, time.Hour)
	expired, _, err := tokens.issue("uid-1", "budi", tokenTypeAccess, "", -time.Minute)
	if err != nil {
		t.Fatalf("issue expired token: %v", err)
	}
	if _, err := tokens.ParseAccess(expired); err == nil {
		t.Fatal("ParseAccess accepted an expired token")
	}

	handler := NewMiddleware(tokens).RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.Header.Set("Authorization", "Bearer "+expired)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expired token status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestRefreshRotatesAndKillsFamilyOnReuse(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}
	first := login(t, mux, "budi", "password")

	res = doJSON(t, mux, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+first.RefreshToken+`"}`, "")
	if res.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want %d; body = %s", res.Code, http.StatusOK, res.Body.String())
	}
	var second Pair
	if err := json.Unmarshal(res.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if second.RefreshToken == "" || second.RefreshToken == first.RefreshToken {
		t.Fatal("refresh did not rotate the refresh token")
	}

	res = doJSON(t, mux, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+first.RefreshToken+`"}`, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("reused refresh status = %d, want %d", res.Code, http.StatusUnauthorized)
	}

	res = doJSON(t, mux, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+second.RefreshToken+`"}`, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("family-kill refresh status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestLogoutRevokesRefreshToken(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}
	pair := login(t, mux, "budi", "password")

	res = doJSON(t, mux, http.MethodPost, "/auth/logout",
		`{"refresh_token":"`+pair.RefreshToken+`"}`, "")
	if res.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", res.Code, http.StatusNoContent)
	}

	res = doJSON(t, mux, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+pair.RefreshToken+`"}`, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout status = %d, want %d", res.Code, http.StatusUnauthorized)
	}

	res = doJSON(t, mux, http.MethodPost, "/auth/logout", `{"refresh_token":"garbage"}`, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("logout with garbage status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestAccessTokenCannotRefresh(t *testing.T) {
	mux := newTestMux()
	res := doJSON(t, mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@example.com","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}
	pair := login(t, mux, "budi", "password")

	res = doJSON(t, mux, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+pair.AccessToken+`"}`, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("access token as refresh status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestAccessTokenExpiresAfter24Hours(t *testing.T) {
	tokens := NewTokens(testSecret, 24*time.Hour, 24*time.Hour)
	pair, _, err := tokens.Issue("uid-1", "budi")
	if err != nil {
		t.Fatalf("issue token pair: %v", err)
	}
	claims, err := tokens.ParseAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("parse fresh access token: %v", err)
	}
	lifetime := time.Until(claims.ExpiresAt.Time)
	if lifetime <= 23*time.Hour || lifetime > 24*time.Hour {
		t.Fatalf("access token lifetime = %v, want about 24h", lifetime)
	}
	if pair.ExpiresIn <= int64((23*time.Hour).Seconds()) || pair.ExpiresIn > int64((24*time.Hour).Seconds()) {
		t.Fatalf("expires_in = %d, want about 86400 seconds", pair.ExpiresIn)
	}
}

type captureMailer struct {
	mu       sync.Mutex
	messages []mailer.Message
	err      error
}

func (c *captureMailer) Send(_ context.Context, msg mailer.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.messages = append(c.messages, msg)
	return nil
}

func (c *captureMailer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.messages)
}

func (c *captureMailer) last() mailer.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.messages[len(c.messages)-1]
}

type magicTestRig struct {
	mux        *http.ServeMux
	magic      *Magic
	magicStore *MemoryMagicStore
	mailer     *captureMailer
	logBuf     *bytes.Buffer
}

func newMagicTestRig() *magicTestRig {
	userStore := user.NewMemoryStore()
	userSvc := user.NewService(userStore)
	tokens := NewTokens(testSecret, 24*time.Hour, 24*time.Hour)
	refreshStore := NewMemoryRefreshStore()
	magicStore := NewMemoryMagicStore()
	mail := &captureMailer{}
	magic := NewMagic(
		userSvc,
		magicStore,
		tokens,
		refreshStore,
		mail,
		"https://ai-sweg.my.id/login/magic",
		15*time.Minute,
	)

	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, nil))
	mux := http.NewServeMux()
	user.NewHandler(userSvc, log).RegisterRoutes(mux, NewMiddleware(tokens).RequireAuth)
	h := NewHandler(NewService(userSvc, tokens, refreshStore), log)
	h.SetMagic(magic)
	h.RegisterRoutes(mux)

	return &magicTestRig{
		mux:        mux,
		magic:      magic,
		magicStore: magicStore,
		mailer:     mail,
		logBuf:     &logBuf,
	}
}

func extractTokenFromMail(t *testing.T, msg mailer.Message) string {
	t.Helper()
	for _, line := range strings.Split(msg.Text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "http://") {
			u, err := url.Parse(line)
			if err != nil {
				t.Fatalf("parse magic link URL %q: %v", line, err)
			}
			token := u.Query().Get("token")
			if token == "" {
				t.Fatalf("magic link URL %q misses token param", line)
			}
			return token
		}
	}
	t.Fatalf("no URL found in email text: %q", msg.Text)
	return ""
}

func TestMagicLinkRequestRegisteredAndUnregisteredShareIdenticalResponse(t *testing.T) {
	rig := newMagicTestRig()
	res := doJSON(t, rig.mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@test.id","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}

	regRes := doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link", `{"email":"Budi@Test.ID"}`, "")
	unregRes := doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link", `{"email":"nobody@test.id"}`, "")

	if regRes.Code != http.StatusOK || unregRes.Code != http.StatusOK {
		t.Fatalf("statuses = (%d, %d), want (200, 200)", regRes.Code, unregRes.Code)
	}
	if regRes.Body.String() != unregRes.Body.String() {
		t.Fatalf("responses differ:\nregistered:   %q\nunregistered: %q", regRes.Body.String(), unregRes.Body.String())
	}
	if rig.mailer.count() != 1 {
		t.Fatalf("sent emails = %d, want 1 (only registered user)", rig.mailer.count())
	}
}

func TestMagicLinkRateLimitRejectsSixthRequestWithinHour(t *testing.T) {
	rig := newMagicTestRig()
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	rig.magic.now = func() time.Time { return now }

	res := doJSON(t, rig.mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@test.id","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}

	for i := 1; i <= 5; i++ {
		res = doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link", `{"email":"budi@test.id"}`, "")
		if res.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200; body = %s", i, res.Code, res.Body.String())
		}
	}

	sixth := doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link", `{"email":"BUDI@test.id"}`, "")
	if sixth.Code != http.StatusTooManyRequests {
		t.Fatalf("6th request status = %d, want %d; body = %s", sixth.Code, http.StatusTooManyRequests, sixth.Body.String())
	}
	if got := strings.TrimSpace(sixth.Body.String()); got != "too many requests" {
		t.Fatalf("6th request body = %q, want %q", got, "too many requests")
	}

	now = now.Add(61 * time.Minute)
	afterWindow := doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link", `{"email":"budi@test.id"}`, "")
	if afterWindow.Code != http.StatusOK {
		t.Fatalf("after 1h window status = %d, want 200; body = %s", afterWindow.Code, afterWindow.Body.String())
	}
}

func TestMagicLinkVerifyHappyPathAndSingleUse(t *testing.T) {
	rig := newMagicTestRig()
	res := doJSON(t, rig.mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@test.id","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}

	res = doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link", `{"email":"budi@test.id"}`, "")
	if res.Code != http.StatusOK {
		t.Fatalf("request magic link status = %d, want 200", res.Code)
	}
	msg := rig.mailer.last()
	rawToken := extractTokenFromMail(t, msg)
	if len(rawToken) != 43 {
		t.Fatalf("token length = %d, want 43 chars base64url", len(rawToken))
	}
	if _, storedRaw := rig.magicStore.records[rawToken]; storedRaw {
		t.Fatal("raw token was stored in MagicStore instead of its SHA-256 hash")
	}
	if msg.IdempotencyKey != hashMagicToken(rawToken) {
		t.Fatalf("IdempotencyKey = %q, want token SHA-256 hash", msg.IdempotencyKey)
	}

	verifyRes := doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link/verify",
		`{"token":"`+rawToken+`"}`, "")
	if verifyRes.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200; body = %s", verifyRes.Code, verifyRes.Body.String())
	}
	var pair Pair
	if err := json.Unmarshal(verifyRes.Body.Bytes(), &pair); err != nil {
		t.Fatalf("decode verify token pair: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" || pair.TokenType != "Bearer" {
		t.Fatalf("unexpected token pair: %+v", pair)
	}

	usersRes := doJSON(t, rig.mux, http.MethodGet, "/users", "", pair.AccessToken)
	if usersRes.Code != http.StatusOK {
		t.Fatalf("GET /users with magic-link access token = %d, want 200", usersRes.Code)
	}

	refreshRes := doJSON(t, rig.mux, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+pair.RefreshToken+`"}`, "")
	if refreshRes.Code != http.StatusOK {
		t.Fatalf("POST /auth/refresh with magic-link refresh token = %d, want 200", refreshRes.Code)
	}
}

func TestMagicLinkVerifyUnknownExpiredAndUsedShareGeneric401(t *testing.T) {
	rig := newMagicTestRig()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	rig.magic.now = func() time.Time { return now }

	res := doJSON(t, rig.mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@test.id","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}

	doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link", `{"email":"budi@test.id"}`, "")
	usedToken := extractTokenFromMail(t, rig.mailer.last())
	first := doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link/verify", `{"token":"`+usedToken+`"}`, "")
	if first.Code != http.StatusOK {
		t.Fatalf("first verify status = %d, want 200", first.Code)
	}

	doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link", `{"email":"budi@test.id"}`, "")
	expiredToken := extractTokenFromMail(t, rig.mailer.last())
	now = now.Add(16 * time.Minute)

	for name, token := range map[string]string{
		"unknown": "dGhpcy10b2tlbi1kb2VzLW5vdC1leGlzdC1pbi10aGUtZGI",
		"used":    usedToken,
		"expired": expiredToken,
		"empty":   "",
	} {
		res := doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link/verify", `{"token":"`+token+`"}`, "")
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s token status = %d, want 401; body = %s", name, res.Code, res.Body.String())
		}
		if got := strings.TrimSpace(res.Body.String()); got != "invalid or expired link" {
			t.Fatalf("%s token body = %q, want %q", name, got, "invalid or expired link")
		}
	}

	for _, badBody := range []string{`{}`, `{"email":"not-an-email"}`, `{"email":"budi@test.id","extra":1}`} {
		bad := doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link", badBody, "")
		if bad.Code != http.StatusBadRequest {
			t.Fatalf("bad magic-link body %s status = %d, want 400", badBody, bad.Code)
		}
	}
}

func TestMagicLinkVerifyConcurrentRaceOnlyOneWins(t *testing.T) {
	rig := newMagicTestRig()
	res := doJSON(t, rig.mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@test.id","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}

	doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link", `{"email":"budi@test.id"}`, "")
	rawToken := extractTokenFromMail(t, rig.mailer.last())

	const callers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	codes := make([]int, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			rec := doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link/verify", `{"token":"`+rawToken+`"}`, "")
			codes[idx] = rec.Code
		}(i)
	}
	close(start)
	wg.Wait()

	var okCount, unauthCount int
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			okCount++
		case http.StatusUnauthorized:
			unauthCount++
		default:
			t.Fatalf("unexpected status code in race: %d", code)
		}
	}
	if okCount != 1 || unauthCount != callers-1 {
		t.Fatalf("race results: ok=%d unauth=%d, want ok=1 unauth=%d", okCount, unauthCount, callers-1)
	}
}

func TestMagicLinkNeverLogsRawToken(t *testing.T) {
	rig := newMagicTestRig()
	res := doJSON(t, rig.mux, http.MethodPost, "/users/register",
		`{"username":"budi","email":"budi@test.id","password":"password"}`, "")
	if res.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", res.Code, http.StatusCreated)
	}

	doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link", `{"email":"budi@test.id"}`, "")
	rawToken := extractTokenFromMail(t, rig.mailer.last())

	doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link/verify", `{"token":"`+rawToken+`"}`, "")
	doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link/verify", `{"token":"`+rawToken+`"}`, "")

	rig.mailer.err = mailer.ErrSendFailed
	doJSON(t, rig.mux, http.MethodPost, "/auth/magic-link", `{"email":"budi@test.id"}`, "")

	logged := rig.logBuf.String()
	if strings.Contains(logged, rawToken) {
		t.Fatalf("handler log leaked raw magic token: %s", logged)
	}
}

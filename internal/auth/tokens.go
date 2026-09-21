package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	tokenTypeAccess  = "access"
	tokenTypeRefresh = "refresh"
	tokenIssuer      = "sweg-ai-lists-backend"
)

// Claims is the JWT payload. RegisteredClaims carries iss/sub/jti/iat/exp.
type Claims struct {
	TokenType string `json:"token_type"`
	Username  string `json:"username,omitempty"`
	jwt.RegisteredClaims
}

// Tokens signs and verifies HS256 JWTs. The secret arrives once from config.
type Tokens struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewTokens wires the signer. Empty secret or non-positive TTL is a wiring bug.
func NewTokens(secret string, accessTTL, refreshTTL time.Duration) *Tokens {
	if strings.TrimSpace(secret) == "" {
		panic("auth: empty JWT secret")
	}
	if accessTTL <= 0 || refreshTTL <= 0 {
		panic("auth: non-positive token TTL")
	}
	return &Tokens{secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// Issue signs a fresh access+refresh pair for the user.
// It also returns the refresh record the caller must persist for revocation.
func (t *Tokens) Issue(userID, username string) (Pair, RefreshRecord, error) {
	accessToken, accessExp, err := t.issue(userID, username, tokenTypeAccess, "", t.accessTTL)
	if err != nil {
		return Pair{}, RefreshRecord{}, err
	}
	jti, err := newJTI()
	if err != nil {
		return Pair{}, RefreshRecord{}, err
	}
	refreshToken, refreshExp, err := t.issue(userID, username, tokenTypeRefresh, jti, t.refreshTTL)
	if err != nil {
		return Pair{}, RefreshRecord{}, err
	}
	pair := Pair{
		AccessToken:      accessToken,
		TokenType:        "Bearer",
		ExpiresIn:        int64(time.Until(accessExp).Seconds()),
		RefreshToken:     refreshToken,
		RefreshExpiresIn: int64(time.Until(refreshExp).Seconds()),
	}
	return pair, RefreshRecord{JTI: jti, UserID: userID, ExpiresAt: refreshExp}, nil
}

// ParseAccess verifies an access token and returns its claims.
func (t *Tokens) ParseAccess(tokenStr string) (Claims, error) {
	return t.parse(tokenStr, tokenTypeAccess)
}

// ParseRefresh verifies a refresh token. Claims.ID carries the jti.
func (t *Tokens) ParseRefresh(tokenStr string) (Claims, error) {
	return t.parse(tokenStr, tokenTypeRefresh)
}

// issue signs one token. jti stays empty for access tokens.
func (t *Tokens) issue(userID, username, tokenType, jti string, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(ttl)
	claims := Claims{
		TokenType: tokenType,
		Username:  username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Subject:   userID,
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign token: %w", err)
	}
	return token, expiresAt, nil
}

// parse verifies signature, issuer, expiry, and token type in that order.
func (t *Tokens) parse(tokenStr, wantType string) (Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(tokenStr, &claims, func(*jwt.Token) (any, error) {
		return t.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Claims{}, ErrTokenExpired
		}
		return Claims{}, ErrTokenInvalid
	}
	if claims.TokenType != wantType {
		return Claims{}, ErrTokenInvalid
	}
	return claims, nil
}

func newJTI() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("auth: generate jti: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"athletica.ai/api/internal/ids"
)

// Errors returned when an access token cannot be trusted.
var (
	ErrTokenMalformed = errors.New("auth: malformed token")
	ErrTokenSignature = errors.New("auth: bad token signature")
	ErrTokenExpired   = errors.New("auth: token expired")
	ErrTokenClaims    = errors.New("auth: unexpected token claims")
)

// TokenTypeAccess marks the only JWT kind this service issues. Refresh tokens
// are opaque random strings stored server-side, never JWTs.
const TokenTypeAccess = "access"

// clockSkew tolerates a small difference between client and server clocks.
const clockSkew = 30 * time.Second

// Claims is the JWT payload. Deliberately minimal: nothing but the subject is
// trusted downstream, and the subject is the only source of a user ID.
type Claims struct {
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	IssuedAt  int64  `json:"iat"`
	NotBefore int64  `json:"nbf"`
	ExpiresAt int64  `json:"exp"`
	TokenID   string `json:"jti"`
	TokenType string `json:"typ"`
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// TokenIssuer signs and verifies HS256 access tokens.
type TokenIssuer struct {
	secret []byte
	issuer string
	now    func() time.Time
}

// NewTokenIssuer builds an issuer. An empty secret is a programming error and
// is rejected here; config.Load already refuses unsafe secrets earlier.
func NewTokenIssuer(secret, issuer string, now func() time.Time) (*TokenIssuer, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("auth: token secret must not be empty")
	}
	if now == nil {
		now = time.Now
	}
	return &TokenIssuer{secret: []byte(secret), issuer: issuer, now: now}, nil
}

// Issue mints an access token for subject valid for ttl.
func (t *TokenIssuer) Issue(subject string, ttl time.Duration) (string, time.Time, error) {
	if subject == "" {
		return "", time.Time{}, errors.New("auth: token subject must not be empty")
	}
	issued := t.now().UTC()
	expires := issued.Add(ttl)

	claims := Claims{
		Issuer:    t.issuer,
		Subject:   subject,
		IssuedAt:  issued.Unix(),
		NotBefore: issued.Unix(),
		ExpiresAt: expires.Unix(),
		TokenID:   ids.NewUUID(),
		TokenType: TokenTypeAccess,
	}

	headerJSON, err := json.Marshal(jwtHeader{Alg: "HS256", Typ: "JWT"})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: encode header: %w", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: encode claims: %w", err)
	}

	signingInput := encode(headerJSON) + "." + encode(claimsJSON)
	return signingInput + "." + encode(t.sign(signingInput)), expires, nil
}

// Parse verifies signature, algorithm, type and validity window.
func (t *TokenIssuer) Parse(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrTokenMalformed
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrTokenMalformed
	}
	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return Claims{}, ErrTokenMalformed
	}
	// Never honour the algorithm a caller asks for beyond the one we issue:
	// this closes the classic "alg: none" / HS-vs-RS confusion hole.
	if header.Alg != "HS256" || (header.Typ != "" && header.Typ != "JWT") {
		return Claims{}, ErrTokenSignature
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrTokenMalformed
	}
	expected := t.sign(parts[0] + "." + parts[1])
	if subtle.ConstantTimeCompare(signature, expected) != 1 {
		return Claims{}, ErrTokenSignature
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrTokenMalformed
	}
	var claims Claims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return Claims{}, ErrTokenMalformed
	}

	if claims.Subject == "" || claims.TokenType != TokenTypeAccess {
		return Claims{}, ErrTokenClaims
	}
	if t.issuer != "" && claims.Issuer != t.issuer {
		return Claims{}, ErrTokenClaims
	}

	now := t.now().UTC()
	if claims.ExpiresAt == 0 || now.After(time.Unix(claims.ExpiresAt, 0).Add(clockSkew)) {
		return Claims{}, ErrTokenExpired
	}
	if claims.NotBefore != 0 && now.Add(clockSkew).Before(time.Unix(claims.NotBefore, 0)) {
		return Claims{}, ErrTokenClaims
	}
	return claims, nil
}

func (t *TokenIssuer) sign(input string) []byte {
	mac := hmac.New(sha256.New, t.secret)
	mac.Write([]byte(input))
	return mac.Sum(nil)
}

func encode(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

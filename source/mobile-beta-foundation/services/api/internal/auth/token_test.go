package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"athletica.ai/api/internal/auth"
)

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestIssueAndParseRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	issuer, err := auth.NewTokenIssuer("a-sufficiently-long-test-secret-value", "athletica-api", fixedClock(now))
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}

	token, expires, err := issuer.Issue("user-1", 15*time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !expires.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("expiry = %s", expires)
	}

	claims, err := issuer.Parse(token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Fatalf("subject = %q", claims.Subject)
	}
	if claims.TokenType != auth.TokenTypeAccess {
		t.Fatalf("typ = %q", claims.TokenType)
	}
}

func TestParseRejectsForeignSecret(t *testing.T) {
	now := time.Now()
	mine, _ := auth.NewTokenIssuer("secret-number-one-that-is-long-enough", "athletica-api", fixedClock(now))
	theirs, _ := auth.NewTokenIssuer("secret-number-two-that-is-long-enough", "athletica-api", fixedClock(now))

	token, _, err := theirs.Issue("attacker", time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := mine.Parse(token); !errors.Is(err, auth.ErrTokenSignature) {
		t.Fatalf("err = %v, want ErrTokenSignature", err)
	}
}

func TestParseRejectsTamperedSubject(t *testing.T) {
	now := time.Now()
	issuer, _ := auth.NewTokenIssuer("a-sufficiently-long-test-secret-value", "athletica-api", fixedClock(now))

	token, _, _ := issuer.Issue("victim", time.Minute)
	parts := strings.Split(token, ".")

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	claims["sub"] = "somebody-else"
	patched, _ := json.Marshal(claims)
	parts[1] = base64.RawURLEncoding.EncodeToString(patched)

	if _, err := issuer.Parse(strings.Join(parts, ".")); !errors.Is(err, auth.ErrTokenSignature) {
		t.Fatalf("err = %v, want ErrTokenSignature for a re-written subject", err)
	}
}

func TestParseRejectsAlgNone(t *testing.T) {
	now := time.Now()
	issuer, _ := auth.NewTokenIssuer("a-sufficiently-long-test-secret-value", "athletica-api", fixedClock(now))

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"attacker","typ":"access","exp":9999999999}`))

	if _, err := issuer.Parse(header + "." + payload + "."); err == nil {
		t.Fatal("alg=none token must be rejected")
	}
}

func TestParseRejectsExpiredToken(t *testing.T) {
	issued := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	clock := issued
	issuer, _ := auth.NewTokenIssuer("a-sufficiently-long-test-secret-value", "athletica-api", func() time.Time { return clock })

	token, _, _ := issuer.Issue("user-1", 15*time.Minute)
	clock = issued.Add(16 * time.Minute)

	if _, err := issuer.Parse(token); !errors.Is(err, auth.ErrTokenExpired) {
		t.Fatalf("err = %v, want ErrTokenExpired", err)
	}
}

func TestParseRejectsMalformedToken(t *testing.T) {
	issuer, _ := auth.NewTokenIssuer("a-sufficiently-long-test-secret-value", "athletica-api", time.Now)
	for _, token := range []string{"", "abc", "a.b", "a.b.c.d", "!!.??.**"} {
		if _, err := issuer.Parse(token); err == nil {
			t.Fatalf("token %q must not parse", token)
		}
	}
}

func TestNewTokenIssuerRejectsEmptySecret(t *testing.T) {
	if _, err := auth.NewTokenIssuer("   ", "athletica-api", time.Now); err == nil {
		t.Fatal("an empty signing secret must be rejected")
	}
}

package httpapi_test

import (
	"bytes"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestRegisterLoginRefreshFlow(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")

	// The access token identifies the caller.
	me := h.send(request{method: http.MethodGet, path: basePath + "/auth/me", token: acc.accessToken})
	if me.status != http.StatusOK {
		t.Fatalf("GET /auth/me: status %d, body %s", me.status, me.body)
	}
	if got := me.str(t, "id"); got != acc.id {
		t.Fatalf("/auth/me returned %q, want the registered id %q", got, acc.id)
	}

	// Login returns a fresh session for the same account.
	login := h.send(request{method: http.MethodPost, path: basePath + "/auth/login", body: map[string]any{
		"email": "ATHLETE@example.com", "password": acc.password,
	}})
	if login.status != http.StatusOK {
		t.Fatalf("login: status %d, body %s", login.status, login.body)
	}
	if got := login.str(t, "user", "id"); got != acc.id {
		t.Fatalf("login returned user %q, want %q", got, acc.id)
	}

	// Refresh rotates the refresh token.
	refresh := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{
		"refreshToken": acc.refreshToken,
	}})
	if refresh.status != http.StatusOK {
		t.Fatalf("refresh: status %d, body %s", refresh.status, refresh.body)
	}
	rotated := refresh.str(t, "refreshToken")
	if rotated == acc.refreshToken {
		t.Fatal("refresh must rotate the refresh token")
	}

	// Replaying the spent token fails and revokes the whole family.
	replay := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{
		"refreshToken": acc.refreshToken,
	}})
	if replay.status != http.StatusUnauthorized {
		t.Fatalf("replayed refresh token: status %d, want 401", replay.status)
	}
	afterReplay := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{
		"refreshToken": rotated,
	}})
	if afterReplay.status != http.StatusUnauthorized {
		t.Fatalf("after a replay every session must be revoked, got status %d", afterReplay.status)
	}
}

func TestAccessTokenExpires(t *testing.T) {
	h := newHarness(t, map[string]string{"ATHLETICA_ACCESS_TOKEN_TTL": "15m"})
	acc := h.register("athlete@example.com", "correct-horse-battery")

	h.clock.Advance(16 * time.Minute)

	res := h.send(request{method: http.MethodGet, path: basePath + "/auth/me", token: acc.accessToken})
	if res.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for an expired access token", res.status)
	}
}

func TestRegisterValidatesInput(t *testing.T) {
	h := newHarness(t, nil)

	cases := []struct {
		name  string
		body  map[string]any
		want  int
		field string
	}{
		{"invalid email", map[string]any{"email": "not-an-email", "password": "correct-horse-battery"}, http.StatusUnprocessableEntity, "email"},
		{"short password", map[string]any{"email": "a@example.com", "password": "short"}, http.StatusUnprocessableEntity, "password"},
		{"missing password", map[string]any{"email": "a@example.com"}, http.StatusUnprocessableEntity, "password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := h.send(request{method: http.MethodPost, path: basePath + "/auth/register", body: tc.body})
			if res.status != tc.want {
				t.Fatalf("status = %d, want %d; body %s", res.status, tc.want, res.body)
			}
			if !bytes.Contains(res.body, []byte(tc.field)) {
				t.Fatalf("body %s should name the offending field %q", res.body, tc.field)
			}
		})
	}
}

func TestRegisterRejectsMalformedBody(t *testing.T) {
	h := newHarness(t, nil)

	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/register", body: "{not json"})
	if res.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.status)
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	h := newHarness(t, nil)
	h.register("athlete@example.com", "correct-horse-battery")

	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/register", body: map[string]any{
		"email": "Athlete@Example.com", "password": "another-good-password",
	}})
	if res.status != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body %s", res.status, res.body)
	}
}

// Audit finding H4: an unknown address and a wrong password must be
// indistinguishable — same status, same headers, byte-identical body.
func TestLoginDoesNotRevealWhetherAnAccountExists(t *testing.T) {
	h := newHarness(t, nil)
	h.register("athlete@example.com", "correct-horse-battery")

	wrongPassword := h.send(request{method: http.MethodPost, path: basePath + "/auth/login", body: map[string]any{
		"email": "athlete@example.com", "password": "not-the-password",
	}, ip: "198.51.100.10"})
	unknownAccount := h.send(request{method: http.MethodPost, path: basePath + "/auth/login", body: map[string]any{
		"email": "nobody@example.com", "password": "not-the-password",
	}, ip: "198.51.100.11"})
	malformedEmail := h.send(request{method: http.MethodPost, path: basePath + "/auth/login", body: map[string]any{
		"email": "]][[", "password": "not-the-password",
	}, ip: "198.51.100.12"})

	for _, res := range []response{wrongPassword, unknownAccount, malformedEmail} {
		if res.status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401; body %s", res.status, res.body)
		}
	}
	if !bytes.Equal(wrongPassword.body, unknownAccount.body) {
		t.Fatalf("bodies differ:\n wrong password: %s\n unknown account: %s", wrongPassword.body, unknownAccount.body)
	}
	if !bytes.Equal(wrongPassword.body, malformedEmail.body) {
		t.Fatalf("bodies differ:\n wrong password: %s\n malformed email: %s", wrongPassword.body, malformedEmail.body)
	}
	if wrongPassword.header.Get("Content-Type") != unknownAccount.header.Get("Content-Type") {
		t.Fatal("content types differ between the two failure modes")
	}
}

// Audit finding H4: brute force from one IP is capped by the request budget.
func TestLoginIsThrottledPerIP(t *testing.T) {
	h := newHarness(t, map[string]string{
		"ATHLETICA_AUTH_RATE_LIMIT":  "3",
		"ATHLETICA_AUTH_RATE_WINDOW": "1m",
	})
	h.register("athlete@example.com", "correct-horse-battery")

	attempt := func(email string) response {
		return h.send(request{method: http.MethodPost, path: basePath + "/auth/login", body: map[string]any{
			"email": email, "password": "wrong-password-here",
		}, ip: "203.0.113.7"})
	}

	// Spread across different accounts so only the IP budget can stop it.
	for i, email := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		if res := attempt(email); res.status != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401", i+1, res.status)
		}
	}
	res := attempt("d@example.com")
	if res.status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 once the per-IP budget is spent", res.status)
	}
	if res.header.Get("Retry-After") == "" {
		t.Fatal("a 429 must carry Retry-After")
	}

	// A different IP is unaffected...
	other := h.send(request{method: http.MethodPost, path: basePath + "/auth/login", body: map[string]any{
		"email": "a@example.com", "password": "wrong-password-here",
	}, ip: "203.0.113.8"})
	if other.status != http.StatusUnauthorized {
		t.Fatalf("an unrelated IP must not be throttled, got %d", other.status)
	}

	// ...and the budget refills once the window elapses.
	h.clock.Advance(time.Minute)
	if res := attempt("e@example.com"); res.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 after the window rolled over", res.status)
	}
}

// Audit finding H4: password spraying one account from many IPs must also stop.
func TestLoginIsThrottledPerAccountAcrossIPs(t *testing.T) {
	h := newHarness(t, map[string]string{
		"ATHLETICA_AUTH_FAILURE_LIMIT": "3",
		"ATHLETICA_AUTH_BACKOFF_BASE":  "30s",
		"ATHLETICA_AUTH_BACKOFF_MAX":   "10m",
	})
	acc := h.register("athlete@example.com", "correct-horse-battery")

	ips := []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"}
	for i, ip := range ips {
		res := h.send(request{method: http.MethodPost, path: basePath + "/auth/login", body: map[string]any{
			"email": acc.email, "password": "wrong-password-here",
		}, ip: ip})
		if res.status != http.StatusUnauthorized {
			t.Fatalf("attempt %d from %s: status %d, want 401", i+1, ip, res.status)
		}
	}

	// A brand-new IP still hits the account lockout.
	blocked := h.send(request{method: http.MethodPost, path: basePath + "/auth/login", body: map[string]any{
		"email": acc.email, "password": "wrong-password-here",
	}, ip: "192.0.2.99"})
	if blocked.status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 for a locked-out account", blocked.status)
	}

	// Even the correct password is refused while the lockout lasts.
	correct := h.send(request{method: http.MethodPost, path: basePath + "/auth/login", body: map[string]any{
		"email": acc.email, "password": acc.password,
	}, ip: "192.0.2.100"})
	if correct.status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 while the account is locked out", correct.status)
	}

	// The backoff expires and the account works again.
	h.clock.Advance(31 * time.Second)
	recovered := h.send(request{method: http.MethodPost, path: basePath + "/auth/login", body: map[string]any{
		"email": acc.email, "password": acc.password,
	}, ip: "192.0.2.100"})
	if recovered.status != http.StatusOK {
		t.Fatalf("status = %d, want 200 after the backoff elapsed; body %s", recovered.status, recovered.body)
	}
}

// The lockout must grow, not stay flat, on repeated failures.
func TestLoginBackoffGrows(t *testing.T) {
	h := newHarness(t, map[string]string{
		"ATHLETICA_AUTH_FAILURE_LIMIT": "1",
		"ATHLETICA_AUTH_BACKOFF_BASE":  "10s",
		"ATHLETICA_AUTH_BACKOFF_MAX":   "10m",
	})
	acc := h.register("athlete@example.com", "correct-horse-battery")

	fail := func() response {
		return h.send(request{method: http.MethodPost, path: basePath + "/auth/login", body: map[string]any{
			"email": acc.email, "password": "wrong-password-here",
		}, ip: "192.0.2.50"})
	}

	fail()
	firstRetry := retryAfterSeconds(t, fail())
	h.clock.Advance(11 * time.Second)
	fail()
	secondRetry := retryAfterSeconds(t, fail())

	if firstRetry <= 0 || secondRetry <= 0 {
		t.Fatalf("expected Retry-After on both lockouts, got %d and %d", firstRetry, secondRetry)
	}
	if secondRetry <= firstRetry {
		t.Fatalf("backoff did not grow: %ds then %ds", firstRetry, secondRetry)
	}
}

func retryAfterSeconds(t *testing.T, res response) int {
	t.Helper()
	raw := res.header.Get("Retry-After")
	if raw == "" {
		t.Fatalf("status %d has no Retry-After header", res.status)
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("Retry-After = %q: %v", raw, err)
	}
	return seconds
}

func TestRefreshIsThrottledPerIP(t *testing.T) {
	h := newHarness(t, map[string]string{"ATHLETICA_AUTH_RATE_LIMIT": "2", "ATHLETICA_AUTH_RATE_WINDOW": "1m"})

	for i := range 2 {
		res := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{
			"refreshToken": "guess",
		}, ip: "203.0.113.20"})
		if res.status != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401", i+1, res.status)
		}
	}
	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{
		"refreshToken": "guess",
	}, ip: "203.0.113.20"})
	if res.status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", res.status)
	}
}

func TestRegisterIsThrottledPerIP(t *testing.T) {
	h := newHarness(t, map[string]string{"ATHLETICA_AUTH_RATE_LIMIT": "2", "ATHLETICA_AUTH_RATE_WINDOW": "1m"})

	for i := range 2 {
		res := h.send(request{method: http.MethodPost, path: basePath + "/auth/register", body: map[string]any{
			"email": "user" + string(rune('a'+i)) + "@example.com", "password": "correct-horse-battery",
		}, ip: "203.0.113.30"})
		if res.status != http.StatusCreated {
			t.Fatalf("attempt %d: status %d, body %s", i+1, res.status, res.body)
		}
	}
	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/register", body: map[string]any{
		"email": "userz@example.com", "password": "correct-horse-battery",
	}, ip: "203.0.113.30"})
	if res.status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", res.status)
	}
}

func TestForgedProxyHeaderCannotResetTheBudget(t *testing.T) {
	h := newHarness(t, map[string]string{"ATHLETICA_AUTH_RATE_LIMIT": "1", "ATHLETICA_AUTH_RATE_WINDOW": "1m"})

	first := h.send(request{method: http.MethodPost, path: basePath + "/auth/login", body: map[string]any{
		"email": "a@example.com", "password": "wrong-password-here",
	}, ip: "203.0.113.40"})
	if first.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", first.status)
	}

	// X-Forwarded-For is ignored unless the deployment declares it is proxied,
	// so a client cannot mint itself a fresh rate-limit bucket per request.
	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/login", body: map[string]any{
		"email": "b@example.com", "password": "wrong-password-here",
	}, ip: "203.0.113.40", headers: map[string]string{"X-Forwarded-For": "10.9.9.9", "X-Real-Ip": "10.9.9.9"}})
	if res.status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", res.status)
	}
}

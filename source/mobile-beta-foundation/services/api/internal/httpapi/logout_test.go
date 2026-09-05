package httpapi_test

import (
	"net/http"
	"testing"
	"time"
)

// TestLogoutRevokesThePresentedRefreshToken is the mobile logout contract: once
// the client has called it, the handle it held is worthless.
func TestLogoutRevokesThePresentedRefreshToken(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")

	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/logout", body: map[string]any{
		"refreshToken": user.refreshToken,
	}})
	if res.status != http.StatusNoContent {
		t.Fatalf("logout status = %d, body %s, want 204", res.status, res.body)
	}
	if len(res.body) != 0 {
		t.Fatalf("logout body = %s, want empty", res.body)
	}

	replay := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{
		"refreshToken": user.refreshToken,
	}})
	if replay.status != http.StatusUnauthorized {
		t.Fatalf("refresh after logout = %d, body %s, want 401", replay.status, replay.body)
	}
}

// TestLogoutIsIndistinguishableForUnknownTokens: the endpoint must not become
// an oracle telling an attacker which refresh handles exist.
func TestLogoutIsIndistinguishableForUnknownTokens(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")

	first := h.send(request{method: http.MethodPost, path: basePath + "/auth/logout", body: map[string]any{
		"refreshToken": user.refreshToken,
	}})
	unknown := h.send(request{method: http.MethodPost, path: basePath + "/auth/logout", body: map[string]any{
		"refreshToken": "not-a-token-anybody-ever-issued",
	}})
	replay := h.send(request{method: http.MethodPost, path: basePath + "/auth/logout", body: map[string]any{
		"refreshToken": user.refreshToken,
	}})

	if first.status != unknown.status || first.status != replay.status {
		t.Fatalf("statuses differ: valid=%d unknown=%d replay=%d — existence leaks",
			first.status, unknown.status, replay.status)
	}
}

// TestLogoutRequiresARefreshToken keeps the payload contract honest.
func TestLogoutRequiresARefreshToken(t *testing.T) {
	h := newHarness(t, nil)
	h.register("athlete@example.com", "correct-horse-battery")

	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/logout", body: map[string]any{}})
	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body %s, want 422", res.status, res.body)
	}
}

// TestLogoutOtherSessionsSurviveASingleLogout: logging out on one device must
// not sign the athlete out of the others.
func TestLogoutOtherSessionsSurviveASingleLogout(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")
	second := h.login(user.email, user.password)

	h.send(request{method: http.MethodPost, path: basePath + "/auth/logout", body: map[string]any{
		"refreshToken": user.refreshToken,
	}})

	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{
		"refreshToken": second.refreshToken,
	}})
	if res.status != http.StatusOK {
		t.Fatalf("the second device = %d, body %s, want 200", res.status, res.body)
	}
}

// TestLogoutAllSessionsViaFlag kills every session from the refresh handle.
func TestLogoutAllSessionsViaFlag(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")
	second := h.login(user.email, user.password)
	third := h.login(user.email, user.password)

	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/logout", body: map[string]any{
		"refreshToken": user.refreshToken,
		"allSessions":  true,
	}})
	if res.status != http.StatusNoContent {
		t.Fatalf("status = %d, body %s, want 204", res.status, res.body)
	}

	for name, token := range map[string]string{"first": user.refreshToken, "second": second.refreshToken, "third": third.refreshToken} {
		got := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{"refreshToken": token}})
		if got.status != http.StatusUnauthorized {
			t.Fatalf("%s session still refreshes (%d): logout-all did not revoke it", name, got.status)
		}
	}
}

// TestLogoutAllEndpointKillsEverySession covers POST /auth/logout-all, whose
// user ID comes from the access token and never from the body.
func TestLogoutAllEndpointKillsEverySession(t *testing.T) {
	h := newHarness(t, nil)
	victim := h.register("athlete@example.com", "correct-horse-battery")
	second := h.login(victim.email, victim.password)
	bystander := h.register("other@example.com", "correct-horse-battery")

	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/logout-all", token: victim.accessToken,
		body: map[string]any{"userId": bystander.id}}) // ignored on purpose
	if res.status != http.StatusNoContent {
		t.Fatalf("status = %d, body %s, want 204", res.status, res.body)
	}

	for name, token := range map[string]string{"first": victim.refreshToken, "second": second.refreshToken} {
		got := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{"refreshToken": token}})
		if got.status != http.StatusUnauthorized {
			t.Fatalf("%s session of the caller still refreshes (%d)", name, got.status)
		}
	}
	// The body named somebody else; their session must be untouched.
	got := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{"refreshToken": bystander.refreshToken}})
	if got.status != http.StatusOK {
		t.Fatalf("a userId in the body revoked another account's session (%d) — the subject must come from the token", got.status)
	}
}

// TestLogoutAllRequiresAnAccessToken guards the authenticated variant.
func TestLogoutAllRequiresAnAccessToken(t *testing.T) {
	h := newHarness(t, nil)
	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/logout-all"})
	if res.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.status)
	}
}

// TestRefreshTokenSweepDropsDeadRows covers the housekeeping that keeps the
// table from growing without bound.
func TestRefreshTokenSweepDropsDeadRows(t *testing.T) {
	h := newHarness(t, map[string]string{
		"ATHLETICA_REFRESH_TOKEN_RETENTION": "0s",
		"ATHLETICA_REFRESH_TOKEN_TTL":       "1h",
	})
	user := h.register("athlete@example.com", "correct-horse-battery")
	live := h.login(user.email, user.password)

	// Revoke the first session, then sweep with no retention window.
	h.send(request{method: http.MethodPost, path: basePath + "/auth/logout", body: map[string]any{"refreshToken": user.refreshToken}})
	h.clock.Advance(time.Second)

	deleted, err := h.api.PruneRefreshTokens(t.Context())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("pruned %d rows, want the single revoked one", deleted)
	}

	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{"refreshToken": live.refreshToken}})
	if res.status != http.StatusOK {
		t.Fatalf("the sweep took a live session with it: %d %s", res.status, res.body)
	}

	// Expired rows go too, once the clock has passed their expiry.
	h.clock.Advance(2 * time.Hour)
	deleted, err = h.api.PruneRefreshTokens(t.Context())
	if err != nil {
		t.Fatalf("second prune: %v", err)
	}
	if deleted == 0 {
		t.Fatal("expired refresh rows were not swept")
	}
}

// TestRefreshingALoggedOutTokenDoesNotKillOtherDevices is a regression test
// for a defect a live run against PostgreSQL exposed.
//
// Rotation replay is a compromise signal and revokes the whole family — that
// must stay. But logout also leaves a revoked row behind, so a background
// refresh racing the logout on the *same* device used to look like a replay and
// signed the athlete out everywhere. A logged-out token is now simply refused.
func TestRefreshingALoggedOutTokenDoesNotKillOtherDevices(t *testing.T) {
	h := newHarness(t, nil)
	phone := h.register("athlete@example.com", "correct-horse-battery")
	tablet := h.login(phone.email, phone.password)

	h.send(request{method: http.MethodPost, path: basePath + "/auth/logout", body: map[string]any{
		"refreshToken": phone.refreshToken,
	}})

	// The phone's in-flight refresh lands after the logout.
	late := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{
		"refreshToken": phone.refreshToken,
	}})
	if late.status != http.StatusUnauthorized {
		t.Fatalf("refresh of a logged-out token = %d, want 401", late.status)
	}

	// The tablet must still be signed in.
	res := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{
		"refreshToken": tablet.refreshToken,
	}})
	if res.status != http.StatusOK {
		t.Fatalf("the tablet was signed out too (%d, %s): a logout must not look like a replay", res.status, res.body)
	}
}

// TestReplayingARotatedTokenStillRevokesTheFamily keeps the compromise
// detection that the fix above must not weaken.
func TestReplayingARotatedTokenStillRevokesTheFamily(t *testing.T) {
	h := newHarness(t, nil)
	phone := h.register("athlete@example.com", "correct-horse-battery")
	tablet := h.login(phone.email, phone.password)

	rotated := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{
		"refreshToken": phone.refreshToken,
	}})
	if rotated.status != http.StatusOK {
		t.Fatalf("rotation failed: %d %s", rotated.status, rotated.body)
	}

	// Replay the token rotation already spent: a stolen-token signal.
	replay := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{
		"refreshToken": phone.refreshToken,
	}})
	if replay.status != http.StatusUnauthorized {
		t.Fatalf("replay = %d, want 401", replay.status)
	}

	for name, token := range map[string]string{
		"the rotated successor": rotated.str(t, "refreshToken"),
		"the other device":      tablet.refreshToken,
	} {
		got := h.send(request{method: http.MethodPost, path: basePath + "/auth/refresh", body: map[string]any{"refreshToken": token}})
		if got.status != http.StatusUnauthorized {
			t.Fatalf("%s survived a replay (%d): the whole family must be revoked", name, got.status)
		}
	}
}

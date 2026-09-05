package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"athletica.ai/api/internal/auth"
	"athletica.ai/api/internal/store"
	"athletica.ai/api/internal/workouts"
)

// genericCredentialsMessage is returned for *every* failed login, whatever the
// real cause (unknown address, wrong password, malformed address). Audit
// finding H4: the response must not tell an attacker which accounts exist.
const genericCredentialsMessage = "invalid email or password"

type credentialsRequest struct {
	Email    *string `json:"email"`
	Password *string `json:"password"`
}

type refreshRequest struct {
	RefreshToken *string `json:"refreshToken"`
}

type userResponse struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	CreatedAt string `json:"createdAt"`
}

type sessionResponse struct {
	User             userResponse `json:"user"`
	AccessToken      string       `json:"accessToken"`
	TokenType        string       `json:"tokenType"`
	ExpiresIn        int          `json:"expiresIn"`
	RefreshToken     string       `json:"refreshToken"`
	RefreshExpiresIn int          `json:"refreshExpiresIn"`
}

func toUserResponse(u store.User) userResponse {
	return userResponse{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339)}
}

func toSessionResponse(u store.User, t auth.Tokens) sessionResponse {
	return sessionResponse{
		User:             toUserResponse(u),
		AccessToken:      t.AccessToken,
		TokenType:        t.TokenType,
		ExpiresIn:        t.ExpiresIn,
		RefreshToken:     t.RefreshToken,
		RefreshExpiresIn: t.RefreshExpiresIn,
	}
}

// handleRegister creates an account. Throttled per IP and per address so the
// endpoint cannot be used to spray accounts or to enumerate existing ones.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	ipKey := "register:ip:" + s.clientIP(r)
	if d := s.ipLimiter.Allow(ipKey); !d.Allowed {
		s.logThrottle(r, "register", "ip", d.Reason)
		writeRateLimited(w, s.log, d.RetryAfter)
		return
	}

	var req credentialsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest, "request body must be a JSON object with email and password")
		return
	}
	email := strings.TrimSpace(deref(req.Email))
	password := deref(req.Password)

	accountKey := "register:account:" + strings.ToLower(email)
	if d := s.accountLimiter.Allow(accountKey); !d.Allowed {
		s.logThrottle(r, "register", "account", d.Reason)
		writeRateLimited(w, s.log, d.RetryAfter)
		return
	}

	user, tokens, err := s.auth.Register(r.Context(), email, password)
	switch {
	case err == nil:
		s.ipLimiter.Succeed(ipKey)
		s.accountLimiter.Succeed(accountKey)
		writeJSON(w, s.log, http.StatusCreated, toSessionResponse(user, tokens))
	case errors.Is(err, auth.ErrInvalidEmail):
		writeValidationError(w, s.log, &workouts.ValidationError{Issues: []workouts.Issue{{Field: "email", Message: "must be a valid email address"}}})
	case errors.Is(err, auth.ErrWeakPassword):
		writeValidationError(w, s.log, &workouts.ValidationError{Issues: []workouts.Issue{{Field: "password", Message: auth.ErrWeakPassword.Error()}}})
	case errors.Is(err, auth.ErrEmailTaken):
		// Registration cannot hide that an address is taken, so at least make
		// probing expensive: a collision counts as a failure for the backoff.
		s.ipLimiter.Fail(ipKey)
		s.accountLimiter.Fail(accountKey)
		writeError(w, s.log, http.StatusConflict, codeEmailTaken, "this email cannot be registered")
	default:
		s.internal(w, r, "register failed", err)
	}
}

// handleLogin exchanges credentials for a session.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ipKey := "login:ip:" + s.clientIP(r)
	if d := s.ipLimiter.Allow(ipKey); !d.Allowed {
		s.logThrottle(r, "login", "ip", d.Reason)
		writeRateLimited(w, s.log, d.RetryAfter)
		return
	}

	var req credentialsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest, "request body must be a JSON object with email and password")
		return
	}
	email := strings.TrimSpace(deref(req.Email))
	password := deref(req.Password)

	accountKey := "login:account:" + strings.ToLower(email)
	if d := s.accountLimiter.Allow(accountKey); !d.Allowed {
		s.logThrottle(r, "login", "account", d.Reason)
		writeRateLimited(w, s.log, d.RetryAfter)
		return
	}

	user, tokens, err := s.auth.Login(r.Context(), email, password)
	switch {
	case err == nil:
		s.ipLimiter.Succeed(ipKey)
		s.accountLimiter.Succeed(accountKey)
		writeJSON(w, s.log, http.StatusOK, toSessionResponse(user, tokens))
	case errors.Is(err, auth.ErrInvalidCredentials):
		s.ipLimiter.Fail(ipKey)
		s.accountLimiter.Fail(accountKey)
		writeError(w, s.log, http.StatusUnauthorized, codeInvalidCreds, genericCredentialsMessage)
	default:
		s.internal(w, r, "login failed", err)
	}
}

// handleRefresh rotates a refresh token. Throttled per IP and per presented
// token so a stolen or guessed handle cannot be brute-forced.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	ipKey := "refresh:ip:" + s.clientIP(r)
	if d := s.ipLimiter.Allow(ipKey); !d.Allowed {
		s.logThrottle(r, "refresh", "ip", d.Reason)
		writeRateLimited(w, s.log, d.RetryAfter)
		return
	}

	var req refreshRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest, "request body must be a JSON object with refreshToken")
		return
	}
	token := strings.TrimSpace(deref(req.RefreshToken))

	// Key on the hash: the raw token never becomes a map key or a log field.
	tokenKey := "refresh:token:" + auth.HashToken(token)
	if d := s.accountLimiter.Allow(tokenKey); !d.Allowed {
		s.logThrottle(r, "refresh", "token", d.Reason)
		writeRateLimited(w, s.log, d.RetryAfter)
		return
	}

	user, tokens, err := s.auth.Refresh(r.Context(), token)
	switch {
	case err == nil:
		s.ipLimiter.Succeed(ipKey)
		s.accountLimiter.Succeed(tokenKey)
		writeJSON(w, s.log, http.StatusOK, toSessionResponse(user, tokens))
	case errors.Is(err, auth.ErrInvalidRefresh):
		s.ipLimiter.Fail(ipKey)
		s.accountLimiter.Fail(tokenKey)
		writeError(w, s.log, http.StatusUnauthorized, codeUnauthorized, "invalid or expired refresh token")
	default:
		s.internal(w, r, "refresh failed", err)
	}
}

// handleMe echoes the authenticated account.
func (s *Server) handleMe(w http.ResponseWriter, _ *http.Request, user store.User) {
	writeJSON(w, s.log, http.StatusOK, toUserResponse(user))
}

func (s *Server) logThrottle(r *http.Request, endpoint, dimension, reason string) {
	// The counter carries only the dimension and the reason — never the key,
	// which is an IP address or an e-mail.
	s.metrics.Throttled(dimension, reason)
	s.log.Warn("auth request throttled",
		"request_id", RequestIDFrom(r.Context()),
		"endpoint", endpoint,
		"dimension", dimension,
		"reason", reason,
	)
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, msg string, err error) {
	s.log.Error(msg, "request_id", RequestIDFrom(r.Context()), "error", err.Error())
	writeError(w, s.log, http.StatusInternalServerError, codeInternal, "internal error")
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

type logoutRequest struct {
	RefreshToken *string `json:"refreshToken"`
	AllSessions  *bool   `json:"allSessions"`
}

// handleLogout ends the session behind the presented refresh token.
//
// It needs no access token — a client logging out may well be holding an
// expired one — and always answers `204`, whatever was presented. An unknown or
// already spent handle is indistinguishable from a valid one, so the endpoint
// cannot be used to probe which refresh tokens exist. Throttled per IP and per
// presented token, exactly like /auth/refresh.
//
// `allSessions: true` revokes every refresh token of the account the presented
// handle belongs to; POST /auth/logout-all does the same from an access token.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	ipKey := "logout:ip:" + s.clientIP(r)
	if d := s.ipLimiter.Allow(ipKey); !d.Allowed {
		s.logThrottle(r, "logout", "ip", d.Reason)
		writeRateLimited(w, s.log, d.RetryAfter)
		return
	}

	var req logoutRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest, "request body must be a JSON object with refreshToken")
		return
	}
	token := strings.TrimSpace(deref(req.RefreshToken))
	if token == "" {
		writeValidationError(w, s.log, &workouts.ValidationError{
			Issues: []workouts.Issue{{Field: "refreshToken", Message: "required"}},
		})
		return
	}

	// Key on the hash: the raw token never becomes a map key or a log field.
	tokenKey := "logout:token:" + auth.HashToken(token)
	if d := s.accountLimiter.Allow(tokenKey); !d.Allowed {
		s.logThrottle(r, "logout", "token", d.Reason)
		writeRateLimited(w, s.log, d.RetryAfter)
		return
	}

	allSessions := req.AllSessions != nil && *req.AllSessions
	if err := s.auth.Logout(r.Context(), token, allSessions); err != nil {
		s.internal(w, r, "logout failed", err)
		return
	}
	s.ipLimiter.Succeed(ipKey)
	s.accountLimiter.Succeed(tokenKey)
	w.WriteHeader(http.StatusNoContent)
}

// handleLogoutAll revokes every refresh token of the authenticated account.
// The user ID comes from the verified access token and from nowhere else.
func (s *Server) handleLogoutAll(w http.ResponseWriter, r *http.Request, user store.User) {
	if err := s.auth.LogoutAll(r.Context(), user.ID); err != nil {
		s.internal(w, r, "logout-all failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

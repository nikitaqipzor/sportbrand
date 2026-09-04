// Package auth owns registration, login, refresh-token rotation and access
// token verification.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"athletica.ai/api/internal/ids"
	"athletica.ai/api/internal/store"
)

// Errors surfaced to the HTTP layer.
var (
	// ErrInvalidCredentials covers unknown e-mail *and* wrong password. The two
	// cases are never distinguishable from outside (audit finding H4).
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrEmailTaken         = errors.New("auth: email already registered")
	ErrInvalidEmail       = errors.New("auth: invalid email address")
	ErrInvalidRefresh     = errors.New("auth: invalid or expired refresh token")
)

// MaxEmailLen bounds the stored address.
const MaxEmailLen = 254

// refreshTokenBytes is the entropy of an opaque refresh token.
const refreshTokenBytes = 32

// Tokens is the credential bundle handed back to a client.
type Tokens struct {
	AccessToken      string
	TokenType        string
	ExpiresIn        int
	RefreshToken     string
	RefreshExpiresIn int
}

// Service implements the auth use cases on top of a Store.
type Service struct {
	store      store.Store
	hasher     *Hasher
	issuer     *TokenIssuer
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
}

// NewService wires the auth service.
func NewService(st store.Store, hasher *Hasher, issuer *TokenIssuer, accessTTL, refreshTTL time.Duration, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: st, hasher: hasher, issuer: issuer, accessTTL: accessTTL, refreshTTL: refreshTTL, now: now}
}

// Register creates an account and signs the caller in.
func (s *Service) Register(ctx context.Context, email, password string) (store.User, Tokens, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return store.User{}, Tokens{}, err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return store.User{}, Tokens{}, err
	}

	user, err := s.store.CreateUser(ctx, store.User{
		ID:           ids.NewUUID(),
		Email:        normalized,
		PasswordHash: hash,
		CreatedAt:    s.now().UTC(),
	})
	if err != nil {
		if errors.Is(err, store.ErrEmailTaken) {
			return store.User{}, Tokens{}, ErrEmailTaken
		}
		return store.User{}, Tokens{}, err
	}

	tokens, err := s.issueTokens(ctx, user.ID)
	if err != nil {
		return store.User{}, Tokens{}, err
	}
	return user, tokens, nil
}

// Login exchanges credentials for tokens. Every failure path returns
// ErrInvalidCredentials after spending comparable CPU time.
func (s *Service) Login(ctx context.Context, email, password string) (store.User, Tokens, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		// A syntactically invalid address must not be distinguishable either.
		s.hasher.VerifyDecoy(password)
		return store.User{}, Tokens{}, ErrInvalidCredentials
	}

	user, err := s.store.UserByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.hasher.VerifyDecoy(password)
			return store.User{}, Tokens{}, ErrInvalidCredentials
		}
		return store.User{}, Tokens{}, err
	}
	if !s.hasher.Verify(user.PasswordHash, password) {
		return store.User{}, Tokens{}, ErrInvalidCredentials
	}

	tokens, err := s.issueTokens(ctx, user.ID)
	if err != nil {
		return store.User{}, Tokens{}, err
	}
	return user, tokens, nil
}

// Refresh rotates a refresh token: the presented one is always revoked, and a
// replay of an already-spent token invalidates the whole family.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (store.User, Tokens, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return store.User{}, Tokens{}, ErrInvalidRefresh
	}

	stored, err := s.store.RefreshTokenByHash(ctx, HashToken(refreshToken))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.User{}, Tokens{}, ErrInvalidRefresh
		}
		return store.User{}, Tokens{}, err
	}

	now := s.now().UTC()
	if stored.RevokedAt != nil {
		// Replay of a spent token: treat as compromise, drop every session.
		if err := s.store.RevokeUserRefreshTokens(ctx, stored.UserID); err != nil {
			return store.User{}, Tokens{}, err
		}
		return store.User{}, Tokens{}, ErrInvalidRefresh
	}
	if !stored.Active(now) {
		return store.User{}, Tokens{}, ErrInvalidRefresh
	}

	user, err := s.store.UserByID(ctx, stored.UserID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.User{}, Tokens{}, ErrInvalidRefresh
		}
		return store.User{}, Tokens{}, err
	}
	if err := s.store.RevokeRefreshToken(ctx, stored.ID); err != nil {
		return store.User{}, Tokens{}, err
	}

	tokens, err := s.issueTokens(ctx, user.ID)
	if err != nil {
		return store.User{}, Tokens{}, err
	}
	return user, tokens, nil
}

// Authenticate resolves a bearer access token to its user. The user ID comes
// from the signed subject only — never from a request body or header.
func (s *Service) Authenticate(ctx context.Context, accessToken string) (store.User, error) {
	claims, err := s.issuer.Parse(accessToken)
	if err != nil {
		return store.User{}, err
	}
	user, err := s.store.UserByID(ctx, claims.Subject)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.User{}, ErrTokenClaims
		}
		return store.User{}, err
	}
	return user, nil
}

func (s *Service) issueTokens(ctx context.Context, userID string) (Tokens, error) {
	access, accessExpiry, err := s.issuer.Issue(userID, s.accessTTL)
	if err != nil {
		return Tokens{}, err
	}

	now := s.now().UTC()
	refresh := ids.NewOpaqueToken(refreshTokenBytes)
	refreshExpiry := now.Add(s.refreshTTL)
	err = s.store.CreateRefreshToken(ctx, store.RefreshToken{
		ID:        ids.NewUUID(),
		UserID:    userID,
		TokenHash: HashToken(refresh),
		IssuedAt:  now,
		ExpiresAt: refreshExpiry,
	})
	if err != nil {
		return Tokens{}, fmt.Errorf("auth: persist refresh token: %w", err)
	}

	return Tokens{
		AccessToken:      access,
		TokenType:        "Bearer",
		ExpiresIn:        int(accessExpiry.Sub(now).Seconds()),
		RefreshToken:     refresh,
		RefreshExpiresIn: int(refreshExpiry.Sub(now).Seconds()),
	}, nil
}

// NormalizeEmail validates and canonicalises an address.
func NormalizeEmail(email string) (string, error) {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" || len(trimmed) > MaxEmailLen {
		return "", ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil || addr.Address != trimmed || !strings.Contains(addr.Address, "@") {
		return "", ErrInvalidEmail
	}
	return store.NormalizeEmail(addr.Address), nil
}

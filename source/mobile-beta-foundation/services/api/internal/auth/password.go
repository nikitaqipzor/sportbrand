package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// Password policy. bcrypt silently ignores anything past 72 bytes, so the
// upper bound is enforced instead of hidden.
const (
	MinPasswordLen = 10
	MaxPasswordLen = 72
)

// ErrWeakPassword is returned when a password fails the policy.
var ErrWeakPassword = fmt.Errorf("auth: password must be between %d and %d bytes", MinPasswordLen, MaxPasswordLen)

// Hasher hashes and verifies passwords with bcrypt.
type Hasher struct {
	cost int
	// decoy is a valid hash of a random value, compared against when no account
	// exists so that "unknown e-mail" and "wrong password" cost the same time.
	decoy []byte
}

// NewHasher builds a Hasher at the given bcrypt cost.
func NewHasher(cost int) (*Hasher, error) {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return nil, fmt.Errorf("auth: bcrypt cost %d is out of range [%d,%d]", cost, bcrypt.MinCost, bcrypt.MaxCost)
	}
	decoy, err := bcrypt.GenerateFromPassword([]byte("athletica-decoy-password"), cost)
	if err != nil {
		return nil, fmt.Errorf("auth: build decoy hash: %w", err)
	}
	return &Hasher{cost: cost, decoy: decoy}, nil
}

// Hash validates the policy and returns a bcrypt hash.
func (h *Hasher) Hash(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(hash), nil
}

// Verify reports whether password matches hash.
func (h *Hasher) Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// VerifyDecoy burns the same amount of CPU as Verify without revealing that no
// account was found (audit finding H4: identical answer for unknown e-mail and
// wrong password, in body *and* in timing).
func (h *Hasher) VerifyDecoy(password string) {
	_ = bcrypt.CompareHashAndPassword(h.decoy, []byte(password))
}

// ValidatePassword applies the password policy.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLen || len(password) > MaxPasswordLen {
		return ErrWeakPassword
	}
	if !utf8.ValidString(password) {
		return errors.New("auth: password must be valid UTF-8")
	}
	return nil
}

// HashToken returns the storage form of an opaque refresh token. Refresh tokens
// are kept as SHA-256 digests so a database dump cannot be replayed.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

package auth_test

import (
	"strings"
	"testing"

	"athletica.ai/api/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

func TestHashAndVerify(t *testing.T) {
	hasher, err := auth.NewHasher(bcrypt.MinCost)
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}

	hash, err := hasher.Hash("correct horse battery")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if strings.Contains(hash, "correct horse") {
		t.Fatal("the hash must not contain the password")
	}
	if !hasher.Verify(hash, "correct horse battery") {
		t.Fatal("the correct password must verify")
	}
	if hasher.Verify(hash, "correct horse batterX") {
		t.Fatal("a wrong password must not verify")
	}
}

func TestPasswordPolicy(t *testing.T) {
	cases := map[string]bool{
		"":                      false,
		"short":                 false,
		"123456789":             false, // 9 bytes
		"1234567890":            true,  // 10 bytes
		strings.Repeat("a", 72): true,
		strings.Repeat("a", 73): false,
		"пароль-достаточно-длинный": true,
	}
	for password, wantOK := range cases {
		err := auth.ValidatePassword(password)
		if wantOK && err != nil {
			t.Errorf("password of %d bytes should be accepted: %v", len(password), err)
		}
		if !wantOK && err == nil {
			t.Errorf("password of %d bytes should be rejected", len(password))
		}
	}
}

func TestHashTokenIsStableAndOpaque(t *testing.T) {
	token := "opaque-refresh-token"
	first := auth.HashToken(token)
	if first != auth.HashToken(token) {
		t.Fatal("HashToken must be deterministic")
	}
	if strings.Contains(first, token) {
		t.Fatal("the stored hash must not embed the token")
	}
	if first == auth.HashToken(token+"x") {
		t.Fatal("different tokens must hash differently")
	}
}

func TestNewHasherRejectsAbsurdCost(t *testing.T) {
	if _, err := auth.NewHasher(0); err == nil {
		t.Fatal("cost 0 must be rejected")
	}
	if _, err := auth.NewHasher(99); err == nil {
		t.Fatal("cost 99 must be rejected")
	}
}

package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=7168,t=5,p=1$") {
		t.Errorf("hash format %s", h)
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Error("salt must differ")
	}
	tests := []struct {
		name, hash, pw string
		mismatch, err  bool
	}{
		{"ok", h, "correct horse battery", false, false},
		{"wrong", h, "correct horse batterY", true, true},
		{"bcrypt", "$2a$10$abc", "x", false, true},
		{"bad version", strings.Replace(h, "v=19", "v=16", 1), "correct horse battery", false, true},
		{"bad params", strings.Replace(h, "m=7168,t=5,p=1", "m=x", 1), "correct horse battery", false, true},
		{"bad salt", strings.Replace(h, "$m=7168,t=5,p=1$", "$m=7168,t=5,p=1$!!", 1), "correct horse battery", false, true},
		{"bad key", h + "!!", "correct horse battery", false, true},
	}
	for _, tc := range tests {
		err := VerifyPassword(tc.hash, tc.pw)
		if (err != nil) != tc.err || errors.Is(err, ErrMismatch) != tc.mismatch {
			t.Errorf("%s: err=%v", tc.name, err)
		}
	}
	if _, err := HashPassword("short"); err == nil {
		t.Error("short password accepted")
	}
	BurnTime("anything")
}

func TestTokens(t *testing.T) {
	a, err := NewToken()
	if err != nil || len(a) != 43 {
		t.Fatalf("token %q %v", a, err)
	}
	b, _ := NewToken()
	if a == b || HashToken(a) == HashToken(b) || len(HashToken(a)) != 64 {
		t.Error("tokens must be unique and hashed to 64 hex chars")
	}
	if !Equal("x", "x") || Equal("x", "y") {
		t.Error("Equal")
	}
}

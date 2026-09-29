package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/voeunkh/gateway-fleet-frontend/internal/auth"
	"github.com/voeunkh/gateway-fleet-frontend/internal/store"
)

func stdinWith(t *testing.T, s string) *os.File {
	t.Helper()
	p := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p) // #nosec G304 -- test temp file
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestUserAdd(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		args  []string
		stdin string
		ok    bool
	}{
		{"ok", []string{"add", "--role", "admin", "alice"}, "correct horse battery\n", true},
		{"no newline ok", []string{"add", "--role", "viewer", "vic"}, "correct horse battery", true},
		{"duplicate", []string{"add", "--role", "admin", "alice"}, "correct horse battery\n", false},
		{"bad role", []string{"add", "--role", "root", "bob"}, "correct horse battery\n", false},
		{"no username", []string{"add", "--role", "admin"}, "correct horse battery\n", false},
		{"short password", []string{"add", "--role", "admin", "carol"}, "short\n", false},
		{"empty stdin", []string{"add", "--role", "admin", "dan"}, "", false},
		{"not add", []string{"remove", "alice"}, "", false},
		{"no subcommand", nil, "", false},
	}
	for _, tc := range tests {
		err := userCmd(ctx, st, tc.args, stdinWith(t, tc.stdin))
		if (err == nil) != tc.ok {
			t.Errorf("%s: err=%v", tc.name, err)
		}
	}
	u, err := st.UserByUsername(ctx, "alice")
	if err != nil || u.Role != "admin" || auth.VerifyPassword(u.PasswordHash, "correct horse battery") != nil {
		t.Errorf("alice: %+v %v", u, err)
	}
}

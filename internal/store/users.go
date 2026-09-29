package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/voeunkh/gateway-fleet-frontend/internal/domain"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrUserExists is returned when a username is taken.
var ErrUserExists = errors.New("user already exists")

// User is a console user.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         domain.Role
	CreatedAt    time.Time
}

// Session is a logged-in session joined with its user.
type Session struct {
	TokenHash string
	CSRF      string
	ExpiresAt time.Time
	User      User
}

// CreateUser adds a user and returns its id.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string, role domain.Role, now time.Time) (string, error) {
	id := ulid.Make().String()
	err := s.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,role,created_at) VALUES(?,?,?,?,?)`,
			id, username, passwordHash, role, now.UnixMilli())
		if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("create user %q: %w", username, ErrUserExists)
		}
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		return insertAudit(ctx, tx, now, id, "user.create", username, string(role))
	})
	return id, err
}

// UserByUsername looks a user up for login.
func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	var created int64
	err := s.read.QueryRowContext(ctx, `SELECT id,username,password_hash,role,created_at FROM users WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("user by name: %w", err)
	}
	u.CreatedAt = time.UnixMilli(created).UTC()
	return u, nil
}

// CreateSession stores a session (by token hash) and audits the login.
func (s *Store) CreateSession(ctx context.Context, tokenHash, csrf, userID string, now, expires time.Time) error {
	return s.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,csrf,created_at,expires_at) VALUES(?,?,?,?,?)`,
			tokenHash, userID, csrf, now.UnixMilli(), expires.UnixMilli()); err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		return insertAudit(ctx, tx, now, userID, "auth.login", "", "")
	})
}

// SessionByTokenHash returns an unexpired session and its user.
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash string, now time.Time) (Session, error) {
	var ss Session
	var exp, created int64
	err := s.read.QueryRowContext(ctx, `
		SELECT s.token_hash, s.csrf, s.expires_at, u.id, u.username, u.role, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, now.UnixMilli()).
		Scan(&ss.TokenHash, &ss.CSRF, &exp, &ss.User.ID, &ss.User.Username, &ss.User.Role, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("session: %w", err)
	}
	ss.ExpiresAt = time.UnixMilli(exp).UTC()
	ss.User.CreatedAt = time.UnixMilli(created).UTC()
	return ss, nil
}

// DeleteSession removes a session (logout). The API audit middleware records it.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	return s.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash); err != nil {
			return fmt.Errorf("delete session: %w", err)
		}
		return nil
	})
}

// DeleteExpiredSessions removes sessions past their expiry and returns how many.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	var n int64
	err := s.Write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.UnixMilli())
		if err != nil {
			return fmt.Errorf("delete expired sessions: %w", err)
		}
		n, err = res.RowsAffected()
		return err
	})
	return n, err
}

// AuditEntry is one audit log row.
type AuditEntry struct {
	At     time.Time
	UserID string
	Action string
	Target string
	Detail string
}

// Audit records a write performed by userID.
func (s *Store) Audit(ctx context.Context, e AuditEntry) error {
	return s.Write(ctx, func(tx *sql.Tx) error {
		return insertAudit(ctx, tx, e.At, e.UserID, e.Action, e.Target, e.Detail)
	})
}

func insertAudit(ctx context.Context, tx *sql.Tx, at time.Time, userID, action, target, detail string) error {
	var uid any
	if userID != "" {
		uid = userID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log(id,at,user_id,action,target,detail) VALUES(?,?,?,?,?,?)`,
		ulid.Make().String(), at.UnixMilli(), uid, action, target, detail); err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	return nil
}

// AuditLog returns the newest audit entries first.
func (s *Store) AuditLog(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT at, COALESCE(user_id,''), action, target, detail FROM audit_log ORDER BY at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("audit log: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var at int64
		if err := rows.Scan(&at, &e.UserID, &e.Action, &e.Target, &e.Detail); err != nil {
			return nil, fmt.Errorf("scan audit: %w", err)
		}
		e.At = time.UnixMilli(at).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

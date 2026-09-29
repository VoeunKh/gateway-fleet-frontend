package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/voeunkh/gateway-fleet-frontend/internal/store"
)

// SessionTTL is how long a login lasts.
const SessionTTL = 12 * time.Hour

// ErrInvalidCredentials is returned for an unknown user or wrong password.
var ErrInvalidCredentials = errors.New("invalid username or password")

// ErrNoSession is returned when a token does not match a live session.
var ErrNoSession = errors.New("no valid session")

// Store is what the service needs from the database.
type Store interface {
	UserByUsername(ctx context.Context, username string) (store.User, error)
	CreateSession(ctx context.Context, tokenHash, csrf, userID string, now, expires time.Time) error
	SessionByTokenHash(ctx context.Context, tokenHash string, now time.Time) (store.Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error
}

// Service logs users in and resolves session cookies.
type Service struct {
	store Store
	now   func() time.Time
}

// NewService builds a Service; now may be nil for time.Now.
func NewService(st Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: st, now: now}
}

// Login checks credentials and creates a session. It returns the cookie token
// (only ever sent to the browser) and the session.
func (s *Service) Login(ctx context.Context, username, password string) (string, store.Session, error) {
	u, err := s.store.UserByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		BurnTime(password)
		return "", store.Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return "", store.Session{}, err
	}
	if err := VerifyPassword(u.PasswordHash, password); err != nil {
		if errors.Is(err, ErrMismatch) {
			return "", store.Session{}, ErrInvalidCredentials
		}
		return "", store.Session{}, fmt.Errorf("verify password: %w", err)
	}
	tok, err := NewToken()
	if err != nil {
		return "", store.Session{}, err
	}
	csrf, err := NewToken()
	if err != nil {
		return "", store.Session{}, err
	}
	now := s.now().UTC()
	sess := store.Session{TokenHash: HashToken(tok), CSRF: csrf, ExpiresAt: now.Add(SessionTTL), User: u}
	if err := s.store.CreateSession(ctx, sess.TokenHash, csrf, u.ID, now, sess.ExpiresAt); err != nil {
		return "", store.Session{}, err
	}
	sess.User.PasswordHash = ""
	return tok, sess, nil
}

// Authenticate resolves a cookie token to its session.
func (s *Service) Authenticate(ctx context.Context, token string) (store.Session, error) {
	if token == "" {
		return store.Session{}, ErrNoSession
	}
	sess, err := s.store.SessionByTokenHash(ctx, HashToken(token), s.now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		return store.Session{}, ErrNoSession
	}
	return sess, err
}

// Logout ends a session.
func (s *Service) Logout(ctx context.Context, sess store.Session) error {
	return s.store.DeleteSession(ctx, sess.TokenHash)
}

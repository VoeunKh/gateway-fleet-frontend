package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/voeunkh/gateway-fleet-frontend/internal/auth"
	"github.com/voeunkh/gateway-fleet-frontend/internal/domain"
	"github.com/voeunkh/gateway-fleet-frontend/internal/store"
)

// Header and cookie names shared with the web UI.
const (
	SessionCookie = "fleet_session"
	CSRFHeader    = "X-CSRF-Token"
)

type ctxKey struct{}

func sessionFrom(ctx context.Context) (store.Session, bool) {
	s, ok := ctx.Value(ctxKey{}).(store.Session)
	return s, ok
}

// authenticate requires a valid session cookie and stores the session in the context.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tok string
		if c, err := r.Cookie(SessionCookie); err == nil {
			tok = c.Value
		}
		sess, err := s.auth.Authenticate(r.Context(), tok)
		if errors.Is(err, auth.ErrNoSession) {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to continue.")
			return
		}
		if err != nil {
			internalError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// requireCSRF checks the X-CSRF-Token header on every non-GET request.
func requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := sessionFrom(r.Context())
		if !safeMethod(r.Method) && !auth.Equal(r.Header.Get(CSRFHeader), sess.CSRF) {
			writeError(w, http.StatusForbidden, "csrf", "Your session token is missing or out of date. Reload the page.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// require allows the request only if the session's role has perm.
//
// sample: const can = a => CAN[S.role].includes(a); title="Your role (X) can't do this"
func require(perm domain.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, ok := sessionFrom(r.Context())
			if !ok || !domain.Can(sess.User.Role, perm) {
				writeError(w, http.StatusForbidden, "forbidden", domain.DenyMessage(sess.User.Role))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// audit writes one audit_log row for every successful write request.
func (s *Server) audit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if safeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status >= 400 {
			return
		}
		sess, _ := sessionFrom(r.Context())
		e := store.AuditEntry{At: s.now().UTC(), UserID: sess.User.ID, Action: r.Method + " " + routePattern(r), Target: r.URL.Path}
		// The response is already sent; use a context that outlives the request.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		if err := s.store.Audit(ctx, e); err != nil {
			slog.Error("audit write failed", "action", e.Action, "target", e.Target, "err", err)
		}
	})
}

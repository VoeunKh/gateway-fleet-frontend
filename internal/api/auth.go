package api

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"

	"github.com/voeunkh/gateway-fleet-frontend/internal/auth"
	"github.com/voeunkh/gateway-fleet-frontend/internal/store"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userJSON struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	RoleName string `json:"roleName"`
}

type sessionJSON struct {
	User userJSON `json:"user"`
	CSRF string   `json:"csrf"`
}

func toSessionJSON(s store.Session) sessionJSON {
	return sessionJSON{
		User: userJSON{Username: s.User.Username, Role: string(s.User.Role), RoleName: s.User.Role.Name()},
		CSRF: s.CSRF,
	}
}

// login: POST /api/login. JSON only, so a cross-site form cannot post it.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "content_type", "Send JSON.")
		return
	}
	var req loginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Enter a username and password.")
		return
	}
	tok, sess, err := s.auth.Login(r.Context(), req.Username, req.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Wrong username or password.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: tok, Path: "/", Expires: sess.ExpiresAt,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secureCookie || r.TLS != nil,
	})
	writeJSON(w, http.StatusOK, toSessionJSON(sess))
}

// session: GET /api/session returns the current user and CSRF token.
func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	writeJSON(w, http.StatusOK, toSessionJSON(sess))
}

// logout: POST /api/logout.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())
	if err := s.auth.Logout(r.Context(), sess); err != nil {
		internalError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secureCookie || r.TLS != nil,
	})
	w.WriteHeader(http.StatusNoContent)
}

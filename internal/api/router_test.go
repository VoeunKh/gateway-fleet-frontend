package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/voeunkh/gateway-fleet-frontend/internal/auth"
	"github.com/voeunkh/gateway-fleet-frontend/internal/domain"
	"github.com/voeunkh/gateway-fleet-frontend/internal/seed"
	"github.com/voeunkh/gateway-fleet-frontend/internal/store"
)

const testPassword = "correct horse battery"

// clock is a settable test clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type env struct {
	t     *testing.T
	store *store.Store
	h     http.Handler
	clock *clock
}

// newSeededEnv is newEnv plus the sample fleet, seeded at the test clock's time.
func newSeededEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, false)
	if err := e.store.Seed(context.Background(), seed.Generate(e.clock.now()), e.clock.now()); err != nil {
		t.Fatal(err)
	}
	return e
}

// newEnv starts a router on a temp DB with one user per role (username = role).
func newEnv(t *testing.T, secure bool) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "fleet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if _, err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{t: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
	for _, r := range []domain.Role{domain.RoleAdmin, domain.RoleRelease, domain.RoleViewer} {
		if _, err := st.CreateUser(ctx, string(r), hash, r, c.now()); err != nil {
			t.Fatal(err)
		}
	}
	static := fstest.MapFS{"index.html": {Data: []byte("<h1>hello</h1>")}}
	return &env{t: t, store: st, h: NewRouter(Config{Store: st, Static: static, SecureCookie: secure, Now: c.now}), clock: c}
}

type client struct {
	cookie *http.Cookie
	csrf   string
}

func (e *env) do(method, path string, body string, c *client, hdr map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c != nil {
		if c.cookie != nil {
			req.AddCookie(c.cookie)
		}
		if c.csrf != "" {
			req.Header.Set(CSRFHeader, c.csrf)
		}
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *env) login(user string) *client {
	e.t.Helper()
	rec := e.do(http.MethodPost, "/api/login", `{"username":"`+user+`","password":"`+testPassword+`"}`, nil, nil)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("login %s: %d %s", user, rec.Code, rec.Body)
	}
	var s sessionJSON
	if err := json.NewDecoder(rec.Body).Decode(&s); err != nil {
		e.t.Fatal(err)
	}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == SessionCookie {
			return &client{cookie: ck, csrf: s.CSRF}
		}
	}
	e.t.Fatal("no session cookie")
	return nil
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var b errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("not an error body: %s", rec.Body)
	}
	return b.Error.Code, b.Error.Message
}

func TestStaticHealthzAndNotFound(t *testing.T) {
	e := newEnv(t, false)
	if rec := e.do(http.MethodGet, "/", "", nil, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "hello") {
		t.Errorf("static: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodGet, "/healthz", "", nil, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Errorf("healthz: %d %s", rec.Code, rec.Body)
	}
	rec := e.do(http.MethodGet, "/api/nope", "", nil, nil)
	if code, _ := errCode(t, rec); rec.Code != 404 || code != "not_found" {
		t.Errorf("404: %d %s", rec.Code, rec.Body)
	}
}

func TestLogin(t *testing.T) {
	e := newEnv(t, false)
	tests := []struct {
		name, body, ctype string
		status            int
		code              string
	}{
		{"wrong password", `{"username":"admin","password":"nope nope nope"}`, "application/json", 401, "invalid_credentials"},
		{"unknown user", `{"username":"ghost","password":"` + testPassword + `"}`, "application/json", 401, "invalid_credentials"},
		{"form post rejected", `username=admin`, "application/x-www-form-urlencoded", 415, "content_type"},
		{"empty fields", `{}`, "application/json", 400, "bad_request"},
		{"bad json", `{`, "application/json", 400, "bad_request"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := e.do(http.MethodPost, "/api/login", tc.body, nil, map[string]string{"Content-Type": tc.ctype})
			if code, _ := errCode(t, rec); rec.Code != tc.status || code != tc.code {
				t.Errorf("%d %s", rec.Code, rec.Body)
			}
			if len(rec.Result().Cookies()) != 0 {
				t.Error("failed login must not set a cookie")
			}
		})
	}

	rec := e.do(http.MethodPost, "/api/login", `{"username":"release","password":"`+testPassword+`"}`, nil, nil)
	var s sessionJSON
	_ = json.NewDecoder(rec.Body).Decode(&s)
	if rec.Code != 200 || s.User.Role != "release" || s.User.RoleName != "Release engineer" || len(s.CSRF) != 43 {
		t.Fatalf("login: %d %+v", rec.Code, s)
	}
	ck := rec.Result().Cookies()[0]
	if ck.Name != SessionCookie || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Secure || ck.Path != "/" {
		t.Errorf("cookie flags: %+v", ck)
	}
	if strings.Contains(rec.Body.String(), ck.Value) {
		t.Error("session token must not be in the body")
	}

	secure := newEnv(t, true)
	if !secure.login("admin").cookie.Secure {
		t.Error("Secure flag not set in prod mode")
	}
}

func TestSession(t *testing.T) {
	e := newEnv(t, false)
	rec := e.do(http.MethodGet, "/api/session", "", nil, nil)
	if code, _ := errCode(t, rec); rec.Code != 401 || code != "unauthenticated" {
		t.Errorf("no cookie: %d %s", rec.Code, rec.Body)
	}
	bogus := &client{cookie: &http.Cookie{Name: SessionCookie, Value: "forged"}}
	if rec := e.do(http.MethodGet, "/api/session", "", bogus, nil); rec.Code != 401 {
		t.Errorf("forged cookie: %d", rec.Code)
	}
	c := e.login("viewer")
	rec = e.do(http.MethodGet, "/api/session", "", c, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"role":"viewer"`) || !strings.Contains(rec.Body.String(), c.csrf) {
		t.Errorf("session: %d %s", rec.Code, rec.Body)
	}
	e.clock.add(auth.SessionTTL + time.Second)
	if rec := e.do(http.MethodGet, "/api/session", "", c, nil); rec.Code != 401 {
		t.Errorf("expired session: %d", rec.Code)
	}
}

func TestCSRFAndLogout(t *testing.T) {
	e := newEnv(t, false)
	c := e.login("admin")
	noToken := &client{cookie: c.cookie}
	wrong := &client{cookie: c.cookie, csrf: "wrong"}
	for name, cl := range map[string]*client{"missing": noToken, "wrong": wrong} {
		rec := e.do(http.MethodPost, "/api/logout", "", cl, nil)
		if code, _ := errCode(t, rec); rec.Code != 403 || code != "csrf" {
			t.Errorf("%s token: %d %s", name, rec.Code, rec.Body)
		}
	}
	rec := e.do(http.MethodPost, "/api/logout", "", c, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", rec.Code, rec.Body)
	}
	if ck := rec.Result().Cookies(); len(ck) != 1 || ck[0].MaxAge >= 0 {
		t.Errorf("logout must clear the cookie: %+v", ck)
	}
	if rec := e.do(http.MethodGet, "/api/session", "", c, nil); rec.Code != 401 {
		t.Errorf("session after logout: %d", rec.Code)
	}
}

// examplePath fills route parameters with sample-looking values.
func examplePath(p string) string {
	return strings.NewReplacer("{id}", "R-001", "{model}", "GW-200", "{version}", "1.2.0",
		"{name}", "gw-agent", "{sn}", "GW200-2438-00101").Replace(p)
}

// TestRBACWriteRoutes is B1's "done when": viewer gets 403 on every write endpoint,
// release gets 403 on remote/ack, admin passes RBAC everywhere.
func TestRBACWriteRoutes(t *testing.T) {
	e := newEnv(t, false)
	clients := map[domain.Role]*client{}
	for _, r := range []domain.Role{domain.RoleAdmin, domain.RoleRelease, domain.RoleViewer} {
		clients[r] = e.login(string(r))
	}
	if len(WriteRoutes) != 15 {
		t.Fatalf("expected 15 write routes, got %d", len(WriteRoutes))
	}
	for _, wr := range WriteRoutes {
		path := examplePath(wr.Pattern)
		if rec := e.do(http.MethodPost, path, "{}", nil, nil); rec.Code != 401 {
			t.Errorf("anonymous %s: %d", path, rec.Code)
		}
		for role, c := range clients {
			rec := e.do(http.MethodPost, path, "{}", c, nil)
			allowed := domain.Can(role, wr.Perm)
			switch {
			case role == domain.RoleViewer && rec.Code != 403,
				role == domain.RoleRelease && (wr.Perm == domain.PermRemote || wr.Perm == domain.PermAck) && rec.Code != 403,
				allowed && rec.Code != http.StatusNotImplemented:
				t.Errorf("%s POST %s: %d %s", role, path, rec.Code, rec.Body)
			}
			if !allowed {
				code, msg := errCode(t, rec)
				if code != "forbidden" || msg != domain.DenyMessage(role) {
					t.Errorf("%s %s: %s %q", role, path, code, msg)
				}
			}
		}
	}
	rec := e.do(http.MethodPost, "/api/devices/X/reboot", "", clients[domain.RoleRelease], nil)
	if _, msg := errCode(t, rec); msg != "Your role (Release engineer) can't do this" {
		t.Errorf("message %q", msg)
	}
}

func TestAuditLog(t *testing.T) {
	e := newEnv(t, false)
	c := e.login("admin")
	v := e.login("viewer")
	e.do(http.MethodPost, "/api/devices/X/reboot", "", v, nil) // 403: not audited
	e.do(http.MethodPost, "/api/devices/X/reboot", "", c, nil) // 501: not audited
	if rec := e.do(http.MethodPost, "/api/logout", "", c, nil); rec.Code != 204 {
		t.Fatal(rec.Code)
	}
	log, err := e.store.AuditLog(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, a := range log {
		actions = append(actions, a.Action)
	}
	// newest first: logout, 2 logins, 3 user creations
	want := "POST /api/logout,auth.login,auth.login,user.create,user.create,user.create"
	if got := strings.Join(actions, ","); got != want {
		t.Errorf("audit actions:\n got %s\nwant %s", got, want)
	}
	if log[0].UserID == "" || log[0].Target != "/api/logout" {
		t.Errorf("logout entry %+v", log[0])
	}
}

func TestLoginBodyLimit(t *testing.T) {
	e := newEnv(t, false)
	big := `{"username":"admin","password":"` + string(bytes.Repeat([]byte("a"), 10000)) + `"}`
	if rec := e.do(http.MethodPost, "/api/login", big, nil, nil); rec.Code != 400 {
		t.Errorf("oversized body: %d", rec.Code)
	}
}

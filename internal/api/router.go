// Package api contains HTTP handlers and routing.
package api

import (
	"io/fs"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/voeunkh/gateway-fleet-frontend/internal/auth"
	"github.com/voeunkh/gateway-fleet-frontend/internal/domain"
	"github.com/voeunkh/gateway-fleet-frontend/internal/fleet"
	"github.com/voeunkh/gateway-fleet-frontend/internal/store"
)

// Config wires the server's dependencies.
type Config struct {
	Store        *store.Store
	Static       fs.FS
	SecureCookie bool             // set the Secure flag on the session cookie (prod / TLS)
	Now          func() time.Time // nil → time.Now
}

// Server holds handler dependencies.
type Server struct {
	store        *store.Store
	auth         *auth.Service
	fleet        *fleet.Service
	secureCookie bool
	now          func() time.Time
}

// WriteRoute is a state-changing endpoint and the permission it needs.
type WriteRoute struct {
	Pattern string
	Perm    domain.Permission
}

// WriteRoutes lists every write endpoint with its permission (sample: CAN).
// Handlers arrive in task B3; until then they answer 501 after auth, CSRF and RBAC.
var WriteRoutes = []WriteRoute{
	{"/api/rollouts", domain.PermRollout},
	{"/api/rollouts/preview", domain.PermRollout},
	{"/api/rollouts/{id}/pause", domain.PermRollout},
	{"/api/rollouts/{id}/resume", domain.PermRollout},
	{"/api/rollouts/{id}/abort", domain.PermRollout},
	{"/api/firmware", domain.PermFirmware},
	{"/api/firmware/{model}/{version}/block", domain.PermFirmware},
	{"/api/firmware/{model}/{version}/unblock", domain.PermFirmware},
	{"/api/packages/{name}/update-outdated", domain.PermPackages},
	{"/api/config/{model}/versions", domain.PermConfig},
	{"/api/config/{model}/push", domain.PermConfig},
	{"/api/devices/{sn}/reboot", domain.PermRemote},
	{"/api/devices/{sn}/logs", domain.PermRemote},
	{"/api/devices/{sn}/ping", domain.PermRemote},
	{"/api/alerts/{id}/ack", domain.PermAck},
}

// ReadRoute is a GET endpoint open to every signed-in role.
type ReadRoute struct {
	Pattern string
	Handler http.HandlerFunc
}

func (s *Server) readRoutes() []ReadRoute {
	return []ReadRoute{
		{"/api/overview", s.overview},
		{"/api/devices", s.devices},
		{"/api/devices/{sn}", s.device},
		{"/api/devices/{sn}/events", s.deviceEvents},
		{"/api/devices/{sn}/metrics", s.deviceMetrics},
		{"/api/firmware", s.firmware},
		{"/api/packages", s.packages},
		{"/api/config/{model}", s.config},
		{"/api/alerts", s.alerts},
		{"/api/rollouts", s.rollouts},
	}
}

// NewRouter builds the HTTP router.
func NewRouter(cfg Config) http.Handler {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	s := &Server{store: cfg.Store, auth: auth.NewService(cfg.Store, now), fleet: fleet.New(cfg.Store, now), secureCookie: cfg.SecureCookie, now: now}

	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Route("/api", func(r chi.Router) {
		// The overview board has one cell per gateway; gzip keeps it small at 10 000 devices.
		r.Use(middleware.Compress(5, "application/json"))
		r.Post("/login", s.login)
		r.Group(func(r chi.Router) {
			r.Use(s.authenticate, requireCSRF, s.audit)
			r.Get("/session", s.session)
			for _, rr := range s.readRoutes() {
				r.Get(rr.Pattern[len("/api"):], rr.Handler)
			}
			r.Post("/logout", s.logout)
			for _, wr := range WriteRoutes {
				r.With(require(wr.Perm)).Post(wr.Pattern[len("/api"):], notImplemented)
			}
		})
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not_found", "No such endpoint.")
		})
	})
	if cfg.Static != nil {
		r.Handle("/*", http.FileServer(http.FS(cfg.Static)))
	}
	return r
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return r.URL.Path
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented", "This action arrives in a later release.")
}

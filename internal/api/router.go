// Package api contains HTTP handlers and routing.
package api

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// NewRouter builds the HTTP router; static serves the embedded web UI.
func NewRouter(static fs.FS) http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	r.Handle("/*", http.FileServer(http.FS(static)))
	return r
}

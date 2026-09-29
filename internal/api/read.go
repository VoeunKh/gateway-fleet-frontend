package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/voeunkh/gateway-fleet-frontend/internal/fleet"
)

// writeResult writes v, or maps a service error to the API error format.
func writeResult(w http.ResponseWriter, r *http.Request, v any, err error) {
	var nf *fleet.ErrNotFound
	var in *fleet.ErrInput
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, v)
	case errors.As(err, &nf):
		writeError(w, http.StatusNotFound, "not_found", nf.Msg)
	case errors.As(err, &in):
		writeError(w, http.StatusBadRequest, "bad_request", in.Msg)
	default:
		internalError(w, r, err)
	}
}

// page reads ?cursor= and ?limit=.
func page(r *http.Request) (fleet.Page, error) {
	p := fleet.Page{Cursor: r.URL.Query().Get("cursor")}
	if l := r.URL.Query().Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil {
			return p, &fleet.ErrInput{Msg: "limit must be a number."}
		}
		p.Limit = n
	}
	return p, nil
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	v, err := s.fleet.Overview(r.Context())
	writeResult(w, r, v, err)
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	p, err := page(r)
	if err != nil {
		writeResult(w, r, nil, err)
		return
	}
	q := r.URL.Query()
	v, err := s.fleet.Devices(r.Context(), fleet.DeviceFilter{Q: q.Get("q"), Model: q.Get("model"), Status: q.Get("status")}, p)
	writeResult(w, r, v, err)
}

func (s *Server) device(w http.ResponseWriter, r *http.Request) {
	v, err := s.fleet.Device(r.Context(), chi.URLParam(r, "sn"))
	writeResult(w, r, v, err)
}

func (s *Server) deviceEvents(w http.ResponseWriter, r *http.Request) {
	p, err := page(r)
	if err != nil {
		writeResult(w, r, nil, err)
		return
	}
	v, err := s.fleet.Events(r.Context(), chi.URLParam(r, "sn"), p)
	writeResult(w, r, v, err)
}

func (s *Server) deviceMetrics(w http.ResponseWriter, r *http.Request) {
	v, err := s.fleet.Metrics(r.Context(), chi.URLParam(r, "sn"), r.URL.Query().Get("range"))
	writeResult(w, r, v, err)
}

func (s *Server) firmware(w http.ResponseWriter, r *http.Request) {
	v, err := s.fleet.Firmware(r.Context())
	writeResult(w, r, map[string]any{"items": v}, err)
}

func (s *Server) packages(w http.ResponseWriter, r *http.Request) {
	v, err := s.fleet.Packages(r.Context())
	writeResult(w, r, map[string]any{"items": v}, err)
}

func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	v, err := s.fleet.Config(r.Context(), chi.URLParam(r, "model"))
	writeResult(w, r, v, err)
}

func (s *Server) alerts(w http.ResponseWriter, r *http.Request) {
	p, err := page(r)
	if err != nil {
		writeResult(w, r, nil, err)
		return
	}
	v, err := s.fleet.Alerts(r.Context(), r.URL.Query().Get("state"), p)
	writeResult(w, r, v, err)
}

func (s *Server) rollouts(w http.ResponseWriter, r *http.Request) {
	p, err := page(r)
	if err != nil {
		writeResult(w, r, nil, err)
		return
	}
	v, err := s.fleet.Rollouts(r.Context(), p)
	writeResult(w, r, v, err)
}

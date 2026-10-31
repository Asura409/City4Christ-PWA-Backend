package app

import (
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/devotionals"
	"net/http"
)

func devotionalRoutes(mux *http.ServeMux, s *devotionals.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	d := devotionalHTTP{s, sessions, limiter, c}
	mux.HandleFunc("GET /api/v1/devotionals", func(w http.ResponseWriter, r *http.Request) {
		if !d.ipLimit(w, r, "public") {
			return
		}
		o, ok := devotionalOptions(w, r)
		if !ok {
			return
		}
		page, err := s.ListPublished(r.Context(), o)
		if err != nil {
			devotionalError(w, err)
			return
		}
		devotionalPage(w, page)
	})
	mux.HandleFunc("GET /api/v1/devotionals/{devotionalId}", func(w http.ResponseWriter, r *http.Request) {
		if !d.ipLimit(w, r, "public") || !devotionalNoQuery(w, r) {
			return
		}
		e, err := s.GetPublished(r.Context(), r.PathValue("devotionalId"))
		if err != nil {
			devotionalError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": e})
	})
	mux.HandleFunc("POST /api/v1/admin/devotionals", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := d.authorize(w, r)
		if !ok || !devotionalNoQuery(w, r) {
			return
		}
		in, ok := decodeJSON[devotionals.Content](w, r)
		if !ok {
			return
		}
		e, err := s.Create(r.Context(), actor, in)
		if err != nil {
			devotionalError(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/admin/devotionals/"+e.ID)
		writeJSON(w, 201, map[string]any{"data": e})
	})
	mux.HandleFunc("GET /api/v1/admin/devotionals", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := d.authorize(w, r)
		if !ok {
			return
		}
		o, ok := devotionalOptions(w, r)
		if !ok {
			return
		}
		page, err := s.ListEditorial(r.Context(), actor, o)
		if err != nil {
			devotionalError(w, err)
			return
		}
		devotionalPage(w, page)
	})
	mux.HandleFunc("GET /api/v1/admin/devotionals/{devotionalId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := d.authorize(w, r)
		if !ok || !devotionalNoQuery(w, r) {
			return
		}
		e, err := s.GetEditorial(r.Context(), actor, r.PathValue("devotionalId"))
		if err != nil {
			devotionalError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": e})
	})
	mux.HandleFunc("PATCH /api/v1/admin/devotionals/{devotionalId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := d.authorize(w, r)
		if !ok || !devotionalNoQuery(w, r) {
			return
		}
		in, ok := decodeJSON[devotionals.Update](w, r)
		if !ok {
			return
		}
		e, err := s.Update(r.Context(), actor, r.PathValue("devotionalId"), in)
		if err != nil {
			devotionalError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": e})
	})
}

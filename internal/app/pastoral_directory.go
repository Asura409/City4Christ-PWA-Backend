package app

import (
	"context"
	"database/sql"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/pastoral"
	"net/http"
)

func pastoralRoutes(mux *http.ServeMux, service *pastoral.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	p := pastoralHTTP{service, sessions, limiter, c}
	mux.HandleFunc("GET /api/v1/pastors", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := p.authorize(w, r); !ok {
			return
		}
		o, _, ok := pastoralOptions(w, r, false)
		if !ok {
			return
		}
		page, err := service.Directory(r.Context(), o)
		if err != nil {
			pastoralError(w, err)
			return
		}
		pastoralPage(w, page)
	})
	mux.HandleFunc("GET /api/v1/me/pastoral/profile", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := p.authorize(w, r)
		if !ok || !pastoralNoQuery(w, r) {
			return
		}
		profile, err := service.OwnProfile(r.Context(), actor)
		if err != nil {
			pastoralError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": profile})
	})
	mux.HandleFunc("POST /api/v1/admin/pastors", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := p.authorize(w, r)
		if !ok || !pastoralNoQuery(w, r) {
			return
		}
		in, ok := decodeJSON[pastoral.ProfileInput](w, r)
		if !ok {
			return
		}
		profile, err := service.Appoint(r.Context(), actor, in, func(ctx context.Context, tx *sql.Tx) error {
			return sessions.RequirePrivilegedTx(ctx, tx, requestAccess(r))
		})
		if err != nil {
			pastoralError(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"data": profile})
	})
	pastoralAvailabilityRoutes(mux, p)
	pastoralBookingRoutes(mux, p)
}

package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func accessRoutes(mux *http.ServeMux, service *access.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, access.ErrForbidden):
			writeError(w, 403, "forbidden", "Access administrator permission is required; self-assignment is prohibited.", nil)
		case errors.Is(err, access.ErrInvalid):
			writeError(w, 422, "validation_failed", "Check target, delegable role, registered scope and reason.", nil)
		default:
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
		}
	}
	authorize := func(w http.ResponseWriter, r *http.Request, write bool) (string, bool) {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Access management is not configured.", nil)
			return "", false
		}
		if write && !originAllowed(c, r.Header.Get("Origin")) {
			writeError(w, 403, "origin_not_allowed", "Supply an allowed Origin header.", nil)
			return "", false
		}
		actor, err := sessions.Authenticate(r.Context(), requestAccess(r))
		if err != nil {
			sessionError(w, err)
			return "", false
		}
		if err = service.Check(r.Context(), actor.ID, "roles.manage", ""); err != nil {
			fail(w, err)
			return "", false
		}
		if !accountLimit(w, r, limiter, "access-management", actor.ID, 30, time.Minute) {
			return "", false
		}
		return actor.ID, true
	}
	mux.HandleFunc("GET /api/v1/admin/accounts/{accountId}/roles", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		result, err := service.List(r.Context(), actor, r.PathValue("accountId"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": result})
	})
	mutation := func(grant bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			actor, ok := authorize(w, r, true)
			if !ok {
				return
			}
			in, ok := decodeJSON[access.Change](w, r)
			if !ok {
				return
			}
			var changed bool
			var err error
			if grant {
				changed, err = service.Grant(r.Context(), actor, r.PathValue("accountId"), in)
			} else {
				changed, err = service.Revoke(r.Context(), actor, r.PathValue("accountId"), in)
			}
			if err != nil {
				fail(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"data": map[string]bool{"changed": changed}})
		}
	}
	mux.HandleFunc("POST /api/v1/admin/accounts/{accountId}/roles", mutation(true))
	mux.HandleFunc("POST /api/v1/admin/accounts/{accountId}/roles/revoke", mutation(false))
}

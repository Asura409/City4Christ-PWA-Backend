package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func churchRoleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, access.ErrLastAdministrator):
		writeError(w, 409, "last_administrator", "Another active Super Admin is required before this appointment can be removed.", nil)
	case errors.Is(err, access.ErrForbidden):
		writeError(w, 403, "forbidden", "Church-role management permission is required for this action.", nil)
	case errors.Is(err, access.ErrInvalid):
		writeError(w, 422, "validation_failed", "Check the verified target, MFA enrollment, role, registered scope and reason.", nil)
	default:
		sessionError(w, err)
	}
}
func churchRoleRoutes(mux *http.ServeMux, service *access.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	authorize := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Church-role management is not configured.", nil)
			return "", false
		}
		if r.Method != "GET" && !originAllowed(c, r.Header.Get("Origin")) {
			writeError(w, 403, "origin_not_allowed", "Supply an allowed Origin header.", nil)
			return "", false
		}
		actor, err := sessions.Authenticate(r.Context(), requestAccess(r))
		if err != nil {
			sessionError(w, err)
			return "", false
		}
		if err = service.Check(r.Context(), actor.ID, "roles.read", ""); err != nil {
			churchRoleError(w, err)
			return "", false
		}
		if !accountLimit(w, r, limiter, "church-role-management", actor.ID, 30, time.Minute) {
			return "", false
		}
		if limiter != nil {
			allowed, err := limiter.Allow(r.Context(), "ip/church-role-management", clientIP(r, c), 120, time.Minute)
			if !limitResult(w, allowed, err) {
				return "", false
			}
		}
		return actor.ID, true
	}
	noQuery := func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			writeError(w, 422, "validation_failed", "This endpoint accepts no query parameters.", nil)
			return false
		}
		return true
	}
	mux.HandleFunc("GET /api/v1/admin/role-candidates", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r)
		if !ok {
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			churchRoleError(w, access.ErrInvalid)
			return
		}
		for name, values := range query {
			if (name != "query" && name != "cursor" && name != "limit") || len(values) != 1 {
				churchRoleError(w, access.ErrInvalid)
				return
			}
		}
		limit := 20
		if values, exists := query["limit"]; exists {
			limit, err = strconv.Atoi(values[0])
			if err != nil {
				churchRoleError(w, access.ErrInvalid)
				return
			}
		}
		page, err := service.Candidates(r.Context(), actor, query.Get("query"), query.Get("cursor"), limit)
		if err != nil {
			churchRoleError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": page.Data, "meta": map[string]string{"nextCursor": page.NextCursor}})
	})
	mux.HandleFunc("GET /api/v1/admin/accounts/{accountId}/church-roles", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r)
		if !ok || !noQuery(w, r) {
			return
		}
		roles, err := service.ChurchList(r.Context(), actor, r.PathValue("accountId"))
		if err != nil {
			churchRoleError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": roles})
	})
	mutation := func(grant bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			actor, ok := authorize(w, r)
			if !ok || !noQuery(w, r) {
				return
			}
			in, ok := decodeJSON[access.Change](w, r)
			if !ok {
				return
			}
			guard := func(ctx context.Context, tx *sql.Tx) error {
				return sessions.RequirePrivilegedTx(ctx, tx, requestAccess(r))
			}
			var changed bool
			var err error
			if grant {
				changed, err = service.ChurchGrant(r.Context(), actor, r.PathValue("accountId"), in, guard)
			} else {
				changed, err = service.ChurchRevoke(r.Context(), actor, r.PathValue("accountId"), in, guard)
			}
			if err != nil {
				churchRoleError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"data": map[string]bool{"changed": changed}})
		}
	}
	mux.HandleFunc("POST /api/v1/admin/accounts/{accountId}/church-roles", mutation(true))
	mux.HandleFunc("POST /api/v1/admin/accounts/{accountId}/church-roles/revoke", mutation(false))
}

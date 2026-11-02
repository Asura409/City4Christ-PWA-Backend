package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/accountdeletion"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

type deletionHTTP struct {
	service  *accountdeletion.Service
	sessions *auth.SessionService
	limiter  *auth.RateLimiter
	config   Config
}

func deletionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, accountdeletion.ErrInvalid):
		writeError(w, 422, "validation_failed", "Confirm the current deletion policy and check the request fields.", nil)
	case errors.Is(err, accountdeletion.ErrNotFound):
		writeError(w, 404, "not_found", "Deletion request not found.", nil)
	case errors.Is(err, accountdeletion.ErrConflict):
		writeError(w, 409, "version_conflict", "This request changed. Reload before reviewing it.", nil)
	case errors.Is(err, accountdeletion.ErrActiveCommitments):
		writeError(w, 409, "blocked_active_commitments", "Cancel or resolve upcoming pastoral appointments before requesting account deletion.", nil)
	case errors.Is(err, access.ErrLastAdministrator):
		writeError(w, 409, "last_administrator", "Another effective Super Admin is required before this account can be disabled.", nil)
	case errors.Is(err, access.ErrForbidden):
		writeError(w, 403, "forbidden", "Account deletion review permission is required.", nil)
	default:
		sessionError(w, err)
	}
}
func (h deletionHTTP) authorize(w http.ResponseWriter, r *http.Request, review bool) (string, bool) {
	if h.sessions == nil || !validOrigin(h.config.AppOrigin) {
		writeError(w, 503, "unavailable", "Session authentication is not configured.", nil)
		return "", false
	}
	if r.Method != "GET" && !originAllowed(h.config, r.Header.Get("Origin")) {
		writeError(w, 403, "origin_not_allowed", "Supply an allowed Origin header.", nil)
		return "", false
	}
	actor, err := h.sessions.Authenticate(r.Context(), requestAccess(r))
	if err != nil {
		sessionError(w, err)
		return "", false
	}
	scope, limit, window := "account-deletion-status", 30, time.Minute
	if review {
		scope = "account-deletion-review"
	} else if r.Method == "DELETE" {
		scope, limit, window = "account-deletion-request", 5, 15*time.Minute
	}
	if !accountLimit(w, r, h.limiter, scope, actor.ID, limit, window) {
		return "", false
	}
	if h.limiter != nil {
		allowed, err := h.limiter.Allow(r.Context(), "ip/"+scope, clientIP(r, h.config), limit*4, window)
		if !limitResult(w, allowed, err) {
			return "", false
		}
	}
	return actor.ID, true
}
func deletionNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		writeError(w, 422, "validation_failed", "This endpoint accepts no query parameters.", nil)
		return false
	}
	return true
}
func (h deletionHTTP) guard(r *http.Request, privileged bool) accountdeletion.Guard {
	return func(ctx context.Context, tx *sql.Tx) error {
		if privileged {
			return h.sessions.RequirePrivilegedTx(ctx, tx, requestAccess(r))
		}
		return h.sessions.RequireSensitiveTx(ctx, tx, requestAccess(r))
	}
}
func accountDeletionRoutes(mux *http.ServeMux, service *accountdeletion.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	h := deletionHTTP{service: service, sessions: sessions, limiter: limiter, config: c}
	mux.HandleFunc("GET /api/v1/me/deletion-request", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := h.authorize(w, r, false)
		if !ok || !deletionNoQuery(w, r) {
			return
		}
		data, err := service.Status(r.Context(), actor)
		if err != nil {
			deletionError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": data})
	})
	mux.HandleFunc("DELETE /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := h.authorize(w, r, false)
		if !ok || !deletionNoQuery(w, r) {
			return
		}
		input, ok := decodeJSON[struct {
			Confirm       bool   `json:"confirm"`
			PolicyVersion string `json:"policyVersion"`
		}](w, r)
		if !ok {
			return
		}
		data, err := service.Request(r.Context(), actor, input.Confirm, input.PolicyVersion, h.guard(r, false))
		if err != nil {
			deletionError(w, err)
			return
		}
		clearRefresh(w, c)
		writeJSON(w, 202, map[string]any{"data": data})
	})
	h.reviewRoutes(mux)
}

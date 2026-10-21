package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/groups"
)

func groupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, groups.ErrForbidden):
		writeError(w, 403, "forbidden", "You do not have permission for this group action.", nil)
	case errors.Is(err, groups.ErrNotFound):
		writeError(w, 404, "group_not_found", "Group not found.", nil)
	case errors.Is(err, groups.ErrConflict):
		writeError(w, 409, "group_conflict", "The church leadership group already exists.", nil)
	case errors.Is(err, groups.ErrInvalid):
		writeError(w, 422, "validation_failed", "Check the group, membership, or pagination fields.", nil)
	default:
		writeError(w, 503, "unavailable", "Service unavailable.", nil)
	}
}

func groupRoutes(mux *http.ServeMux, service *groups.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	authorize := func(w http.ResponseWriter, r *http.Request, write bool) (string, bool) {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Group services are not configured.", nil)
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
		if !accountLimit(w, r, limiter, "groups", actor.ID, 60, time.Minute) {
			return "", false
		}
		if limiter != nil {
			allowed, e := limiter.Allow(r.Context(), "ip/groups", clientIP(r, c), 120, time.Minute)
			if !limitResult(w, allowed, e) {
				return "", false
			}
		}
		return actor.ID, true
	}
	mux.HandleFunc("GET /api/v1/groups", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			groupError(w, groups.ErrInvalid)
			return
		}
		for key, values := range q {
			if key != "limit" && key != "cursor" || len(values) != 1 {
				groupError(w, groups.ErrInvalid)
				return
			}
		}
		limit := 20
		if q.Has("limit") {
			limit, err = strconv.Atoi(q.Get("limit"))
			if err != nil {
				groupError(w, groups.ErrInvalid)
				return
			}
		}
		page, err := service.List(r.Context(), actor, groups.Options{Limit: limit, Cursor: q.Get("cursor")})
		if err != nil {
			groupError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": page.Items, "meta": map[string]string{"nextCursor": page.NextCursor}})
	})
	mux.HandleFunc("POST /api/v1/admin/groups", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			groupError(w, groups.ErrInvalid)
			return
		}
		in, ok := decodeJSON[groups.CreateInput](w, r)
		if !ok {
			return
		}
		group, err := service.Create(r.Context(), actor, in)
		if err != nil {
			groupError(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"data": group})
	})
	mux.HandleFunc("POST /api/v1/groups/{groupId}/membership", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			groupError(w, groups.ErrInvalid)
			return
		}
		if _, ok = decodeJSON[struct{}](w, r); !ok {
			return
		}
		membership, err := service.Join(r.Context(), actor, r.PathValue("groupId"))
		if err != nil {
			groupError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": membership})
	})
	mux.HandleFunc("DELETE /api/v1/groups/{groupId}/membership", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			groupError(w, groups.ErrInvalid)
			return
		}
		if err := service.Leave(r.Context(), actor, r.PathValue("groupId")); err != nil {
			groupError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]string{"status": "left"}})
	})
	mux.HandleFunc("GET /api/v1/groups/{groupId}/membership-requests", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			groupError(w, groups.ErrInvalid)
			return
		}
		requests, err := service.PendingRequests(r.Context(), actor, r.PathValue("groupId"))
		if err != nil {
			groupError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": requests})
	})
	mux.HandleFunc("PATCH /api/v1/groups/{groupId}/membership-requests/{accountId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			groupError(w, groups.ErrInvalid)
			return
		}
		in, ok := decodeJSON[groups.DecisionInput](w, r)
		if !ok {
			return
		}
		changed, err := service.Decide(r.Context(), actor, r.PathValue("groupId"), r.PathValue("accountId"), in)
		if err != nil {
			groupError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]bool{"changed": changed}})
	})
}

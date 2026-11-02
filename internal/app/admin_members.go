package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func adminMembersActor(w http.ResponseWriter, r *http.Request, service *access.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) (string, bool) {
	if sessions == nil || !validOrigin(c.AppOrigin) {
		writeError(w, 503, "unavailable", "Member lookup is not configured.", nil)
		return "", false
	}
	actor, err := sessions.Authenticate(r.Context(), requestAccess(r))
	if err != nil {
		sessionError(w, err)
		return "", false
	}
	if err = service.Check(r.Context(), actor.ID, "members.records.read", ""); err != nil {
		if errors.Is(err, access.ErrForbidden) {
			writeError(w, 403, "forbidden", "Member lookup permission is required.", nil)
		} else {
			sessionError(w, err)
		}
		return "", false
	}
	if !accountLimit(w, r, limiter, "admin-member-search", actor.ID, 30, time.Minute) {
		return "", false
	}
	if limiter != nil {
		allowed, err := limiter.Allow(r.Context(), "ip/admin-member-search", clientIP(r, c), 120, time.Minute)
		if !limitResult(w, allowed, err) {
			return "", false
		}
	}
	return actor.ID, true
}

func adminMemberRoutes(mux *http.ServeMux, service *access.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	mux.HandleFunc("GET /api/v1/admin/members", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := adminMembersActor(w, r, service, sessions, limiter, c)
		if !ok {
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			writeError(w, 422, "validation_failed", "Check search query, cursor and limit.", nil)
			return
		}
		for name, values := range query {
			if (name != "query" && name != "cursor" && name != "limit") || len(values) != 1 {
				writeError(w, 422, "validation_failed", "Check search query, cursor and limit.", nil)
				return
			}
		}
		limit := 20
		if values, ok := query["limit"]; ok {
			limit, err = strconv.Atoi(values[0])
			if err != nil {
				writeError(w, 422, "validation_failed", "Check search query, cursor and limit.", nil)
				return
			}
		}
		page, err := service.SearchMembers(r.Context(), actor, query.Get("query"), query.Get("cursor"), limit)
		if errors.Is(err, access.ErrInvalid) {
			writeError(w, 422, "validation_failed", "Search with at least three characters and a valid cursor and limit.", nil)
			return
		}
		if err != nil {
			sessionError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]any{"data": page.Data, "meta": map[string]string{"nextCursor": page.NextCursor}})
	})
	mux.HandleFunc("GET /api/v1/admin/members/{accountId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := adminMembersActor(w, r, service, sessions, limiter, c)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			writeError(w, 422, "validation_failed", "This endpoint does not accept query parameters.", nil)
			return
		}
		member, err := service.MemberDetail(r.Context(), actor, r.PathValue("accountId"))
		switch {
		case errors.Is(err, access.ErrForbidden):
			writeError(w, 403, "forbidden", "Member lookup permission is required.", nil)
			return
		case errors.Is(err, access.ErrMemberNotFound):
			writeError(w, 404, "not_found", "Active member not found.", nil)
			return
		case err != nil:
			sessionError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]any{"data": member})
	})
}

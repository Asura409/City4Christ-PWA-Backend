package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/profiles"
)

func directoryRoutes(mux *http.ServeMux, service *profiles.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, profiles.ErrInvalidDirectory):
			var validation *profiles.DirectoryValidationError
			fields := map[string]string{"parameters": "Use only query, limit and cursor, once each."}
			if errors.As(err, &validation) {
				fields = validation.Fields
			}
			writeError(w, 422, "validation_failed", "Check the indicated directory parameters.", fields)
		case errors.Is(err, profiles.ErrNotFound):
			writeError(w, 401, "invalid_session", "Sign in again.", nil)
		default:
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
		}
	}
	mux.HandleFunc("GET /api/v1/members", func(w http.ResponseWriter, r *http.Request) {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Session authentication is not configured.", nil)
			return
		}
		actor, err := sessions.Authenticate(r.Context(), requestAccess(r))
		if err != nil {
			sessionError(w, err)
			return
		}
		if !accountLimit(w, r, limiter, "member-directory", actor.ID, 60, time.Minute) {
			return
		}
		if limiter != nil {
			allowed, err := limiter.Allow(r.Context(), "ip/member-directory", clientIP(r, c), 120, time.Minute)
			if !limitResult(w, allowed, err) {
				return
			}
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			fail(w, &profiles.DirectoryValidationError{Fields: map[string]string{"parameters": "Use only query, limit and cursor once each, with valid URL encoding."}})
			return
		}
		for key, values := range q {
			if (key != "query" && key != "cursor" && key != "limit") || len(values) != 1 {
				fail(w, &profiles.DirectoryValidationError{Fields: map[string]string{"parameters": "Use only query, limit and cursor once each, with valid URL encoding."}})
				return
			}
		}
		options := profiles.DirectoryOptions{Query: q.Get("query"), Cursor: q.Get("cursor")}
		if q.Has("limit") {
			options.Limit, err = strconv.Atoi(q.Get("limit"))
			if err != nil || options.Limit < 1 || options.Limit > 100 {
				fail(w, &profiles.DirectoryValidationError{Fields: map[string]string{"limit": "Use an integer from 1 to 100."}})
				return
			}
		}
		page, err := service.Directory(r.Context(), actor.ID, options)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": page.Items, "meta": map[string]string{"nextCursor": page.NextCursor}})
	})
}

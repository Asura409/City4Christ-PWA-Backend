package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/posts"
)

func postRoutes(mux *http.ServeMux, service *posts.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	writeErrorFor := func(w http.ResponseWriter, err error) {
		if errors.Is(err, posts.ErrInvalid) {
			writeError(w, 422, "validation_failed", "Check the post or feed pagination fields.", nil)
			return
		}
		writeError(w, 503, "unavailable", "Service unavailable.", nil)
	}
	authorize := func(w http.ResponseWriter, r *http.Request, write bool) (string, bool) {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Post services are not configured.", nil)
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
		if !accountLimit(w, r, limiter, "posts", actor.ID, 60, time.Minute) {
			return "", false
		}
		if limiter != nil {
			allowed, e := limiter.Allow(r.Context(), "ip/posts", clientIP(r, c), 120, time.Minute)
			if !limitResult(w, allowed, e) {
				return "", false
			}
		}
		return actor.ID, true
	}
	mux.HandleFunc("GET /api/v1/posts", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		_ = actor
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			writeErrorFor(w, posts.ErrInvalid)
			return
		}
		for key, values := range q {
			if (key != "limit" && key != "cursor") || len(values) != 1 {
				writeErrorFor(w, posts.ErrInvalid)
				return
			}
		}
		o := posts.Options{Cursor: q.Get("cursor")}
		if q.Has("limit") {
			o.Limit, err = strconv.Atoi(q.Get("limit"))
			if err != nil || o.Limit < 1 || o.Limit > 100 {
				writeErrorFor(w, posts.ErrInvalid)
				return
			}
		}
		page, err := service.List(r.Context(), o)
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": page.Items, "meta": map[string]string{"nextCursor": page.NextCursor}})
	})
	mux.HandleFunc("POST /api/v1/posts", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeErrorFor(w, posts.ErrInvalid)
			return
		}
		in, ok := decodeJSON[posts.CreateInput](w, r)
		if !ok {
			return
		}
		post, err := service.Create(r.Context(), actor, in)
		if err != nil {
			writeErrorFor(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"data": post})
	})
}

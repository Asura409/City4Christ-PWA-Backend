package app

import (
	"context"
	"database/sql"
	"errors"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/bible"
	"net/http"
	"net/url"
	"time"
)

func bibleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, bible.ErrInvalid):
		fields := map[string]string{}
		var v *bible.ValidationError
		if errors.As(err, &v) {
			fields = v.Fields
		}
		writeError(w, 422, "validation_failed", "Check the Bible reference and translation.", fields)
	case errors.Is(err, bible.ErrRateLimited):
		w.Header().Set("Retry-After", "30")
		writeError(w, 429, "bible_rate_limited", "Bible lookup is busy. Try again shortly.", nil)
	default:
		writeError(w, 503, "bible_unavailable", "Bible lookup is temporarily unavailable. Try again later.", nil)
	}
}
func bibleRoutes(mux *http.ServeMux, db *sql.DB, limiter *auth.RateLimiter, c Config) {
	provider := c.BibleProvider
	if provider == nil {
		provider = bible.NewAPI(nil)
	}
	service := bible.NewService(provider, func(ctx context.Context) (bool, error) {
		if limiter == nil || db == nil {
			return false, bible.ErrUnavailable
		}
		return limiter.Allow(ctx, "bible-provider", "bible-api.com", 12, 30*time.Second)
	}, c.Now)
	allowed := func(w http.ResponseWriter, r *http.Request) bool {
		if limiter == nil || db == nil {
			bibleError(w, bible.ErrUnavailable)
			return false
		}
		ok, err := limiter.Allow(r.Context(), "bible-read", clientIP(r, c), 60, time.Minute)
		return limitResult(w, ok, err)
	}
	mux.HandleFunc("GET /api/v1/bible/translations", func(w http.ResponseWriter, r *http.Request) {
		if !allowed(w, r) {
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			bibleError(w, bible.ErrInvalid)
			return
		}
		writeJSON(w, 200, map[string]any{"data": bible.Translations()})
	})
	mux.HandleFunc("GET /api/v1/bible/passages", func(w http.ResponseWriter, r *http.Request) {
		if !allowed(w, r) {
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			bibleError(w, bible.ErrInvalid)
			return
		}
		for k, v := range q {
			if len(v) != 1 || (k != "reference" && k != "translation") {
				bibleError(w, bible.ErrInvalid)
				return
			}
		}
		passage, err := service.Lookup(r.Context(), bible.Query{Reference: q.Get("reference"), Translation: q.Get("translation")})
		if err != nil {
			bibleError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": passage})
	})
}

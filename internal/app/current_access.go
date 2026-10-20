package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/access"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func currentAccessRoutes(mux *http.ServeMux, service *access.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	mux.HandleFunc("GET /api/v1/me/access", func(w http.ResponseWriter, r *http.Request) {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Session authentication is not configured.", nil)
			return
		}
		actor, err := sessions.Authenticate(r.Context(), requestAccess(r))
		if err != nil {
			sessionError(w, err)
			return
		}
		if !accountLimit(w, r, limiter, "current-access", actor.ID, 60, time.Minute) {
			return
		}
		if limiter != nil {
			allowed, err := limiter.Allow(r.Context(), "ip/current-access", clientIP(r, c), 120, time.Minute)
			if !limitResult(w, allowed, err) {
				return
			}
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			writeError(w, 422, "validation_failed", "This endpoint accepts no query parameters.", map[string]string{"parameters": "Access is returned only for your authenticated account."})
			return
		}
		current, err := service.Current(r.Context(), actor.ID)
		switch {
		case errors.Is(err, access.ErrForbidden):
			sessionError(w, auth.ErrInvalidSession)
		case err != nil:
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
		default:
			writeJSON(w, 200, map[string]any{"data": current})
		}
	})
}

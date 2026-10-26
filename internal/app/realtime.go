package app

import (
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/messaging"
)

func realtimeTicketRoutes(mux *http.ServeMux, tickets *messaging.RealtimeTicketService, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	mux.HandleFunc("POST /api/v1/realtime/tickets", func(w http.ResponseWriter, r *http.Request) {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Realtime messaging is not configured.", nil)
			return
		}
		if !originAllowed(c, r.Header.Get("Origin")) {
			writeError(w, 403, "origin_not_allowed", "Supply an allowed Origin header.", nil)
			return
		}
		actor, sessionID, accessExpiresAt, err := sessions.AuthenticateSession(r.Context(), requestAccess(r))
		if err != nil {
			sessionError(w, err)
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength > 0 {
			writeError(w, 422, "validation_failed", "Ticket requests do not accept a body or query parameters.", nil)
			return
		}
		if !accountLimit(w, r, limiter, "realtime-ticket", actor.ID, 10, time.Minute) {
			return
		}
		if limiter != nil {
			allowed, limitErr := limiter.Allow(r.Context(), "ip/realtime-ticket", clientIP(r, c), 30, time.Minute)
			if !limitResult(w, allowed, limitErr) {
				return
			}
		}
		ticket, err := tickets.Issue(r.Context(), actor.ID, sessionID, accessExpiresAt)
		if err != nil {
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"ticket": ticket.Value, "expiresAt": ticket.ExpiresAt, "expiresIn": int(ticket.ExpiresAt.Sub(timeNow(c)).Seconds())}})
	})
}

func timeNow(c Config) time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

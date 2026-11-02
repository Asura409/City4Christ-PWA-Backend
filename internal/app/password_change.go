package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func passwordChangeRoutes(mux *http.ServeMux, service *auth.PasswordChangeService, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	mux.HandleFunc("POST /api/v1/auth/password/change", func(w http.ResponseWriter, r *http.Request) {
		if service == nil || sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Session authentication is not configured.", nil)
			return
		}
		if !originAllowed(c, r.Header.Get("Origin")) {
			writeError(w, 403, "origin_not_allowed", "Supply an allowed Origin header.", nil)
			return
		}
		actor, err := sessions.Authenticate(r.Context(), requestAccess(r))
		if err != nil {
			sessionError(w, err)
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			writeError(w, 422, "validation_failed", "This endpoint accepts no query parameters.", nil)
			return
		}
		if !accountLimit(w, r, limiter, "password-change", actor.ID, 5, 15*time.Minute) {
			return
		}
		if limiter != nil {
			allowed, err := limiter.Allow(r.Context(), "ip/password-change", clientIP(r, c), 5, 15*time.Minute)
			if !limitResult(w, allowed, err) {
				return
			}
		}
		input, ok := decodeJSON[struct {
			CurrentPassword passwordInput `json:"currentPassword"`
			NewPassword     passwordInput `json:"newPassword"`
		}](w, r)
		if !ok {
			return
		}
		err = service.Change(r.Context(), requestAccess(r), string(input.CurrentPassword), string(input.NewPassword))
		var validation *auth.ValidationError
		switch {
		case errors.Is(err, auth.ErrCurrentPasswordInvalid):
			writeError(w, 422, "current_password_invalid", "Current password is incorrect.", map[string]string{"currentPassword": "Enter your current password."})
		case errors.Is(err, auth.ErrLocalPasswordRequired):
			writeError(w, 409, "local_password_required", "This account uses Google sign-in and has no local password.", nil)
		case errors.As(err, &validation):
			writeError(w, 422, "validation_failed", "Check the supplied fields.", validation.Fields)
		case err != nil:
			sessionError(w, err)
		default:
			clearRefresh(w, c)
			writeJSON(w, 200, map[string]any{"data": map[string]string{"status": "password_changed"}})
		}
	})
}

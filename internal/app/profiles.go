package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/profiles"
)

func profileRoutes(mux *http.ServeMux, service *profiles.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	fail := func(w http.ResponseWriter, err error) {
		var validation *profiles.ValidationError
		switch {
		case errors.As(err, &validation):
			writeError(w, 422, "validation_failed", "Check the indicated profile fields.", validation.Fields)
		case errors.Is(err, profiles.ErrNotFound):
			writeError(w, 401, "invalid_session", "Sign in again.", nil)
		case errors.Is(err, profiles.ErrInvalidAvatarUpload):
			writeError(w, 422, "invalid_avatar_upload", "Choose your own available, safety-checked profile photo upload.", nil)
		default:
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
		}
	}
	authorize := func(w http.ResponseWriter, r *http.Request, write bool) (string, bool) {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Session authentication is not configured.", nil)
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
		if write && !accountLimit(w, r, limiter, "profile-write", actor.ID, 30, time.Minute) {
			return "", false
		}
		return actor.ID, true
	}
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		p, err := service.Get(r.Context(), actor)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": p})
	})
	mux.HandleFunc("PATCH /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		input, ok := decodeJSON[profiles.Update](w, r)
		if !ok {
			return
		}
		p, err := service.Update(r.Context(), actor, input)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": p})
	})
	mux.HandleFunc("PUT /api/v1/me/avatar", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeError(w, 400, "invalid_parameters", "Do not use query parameters for profile photos.", nil)
			return
		}
		input, ok := decodeJSON[struct {
			UploadID string `json:"uploadId"`
		}](w, r)
		if !ok {
			return
		}
		p, err := service.SetAvatar(r.Context(), actor, input.UploadID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": p})
	})
	mux.HandleFunc("DELETE /api/v1/me/avatar", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeError(w, 400, "invalid_parameters", "Do not use query parameters for profile photos.", nil)
			return
		}
		p, err := service.RemoveAvatar(r.Context(), actor)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": p})
	})
}

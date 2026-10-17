package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/sermons"
)

func sermonPublishingRoutes(mux *http.ServeMux, service *sermons.Service, sessions *auth.SessionService, limiter *auth.RateLimiter, c Config) {
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, sermons.ErrForbidden):
			writeError(w, 403, "forbidden", "Content editor permission is required.", nil)
		case errors.Is(err, sermons.ErrNotFound):
			writeError(w, 404, "sermon_not_found", "Sermon not found.", nil)
		case errors.Is(err, sermons.ErrInvalidInput):
			writeError(w, 422, "validation_failed", "Check sermon fields, publication settings and version.", nil)
		case errors.Is(err, sermons.ErrVersionConflict):
			writeError(w, 409, "version_conflict", "The sermon changed. Reload its latest version before editing.", nil)
		default:
			writeError(w, 503, "unavailable", "Service unavailable.", nil)
		}
	}
	authorize := func(w http.ResponseWriter, r *http.Request, write bool) (string, bool) {
		if sessions == nil || !validOrigin(c.AppOrigin) {
			writeError(w, 503, "unavailable", "Editor authentication is not configured.", nil)
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
		if err = service.CanEdit(r.Context(), actor.ID); err != nil {
			fail(w, err)
			return "", false
		}
		if write && !accountLimit(w, r, limiter, "sermon-write", actor.ID, 60, time.Minute) {
			return "", false
		}
		return actor.ID, true
	}
	mux.HandleFunc("POST /api/v1/admin/sermons", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		input, ok := decodeJSON[sermons.DraftInput](w, r)
		if !ok {
			return
		}
		result, err := service.CreateDraft(r.Context(), actor, input)
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/admin/sermons/"+result.ID)
		writeJSON(w, 201, map[string]any{"data": result})
	})
	mux.HandleFunc("GET /api/v1/admin/sermons/{sermonId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, false)
		if !ok {
			return
		}
		result, err := service.GetEditorial(r.Context(), actor, r.PathValue("sermonId"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": result})
	})
	mux.HandleFunc("PATCH /api/v1/admin/sermons/{sermonId}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := authorize(w, r, true)
		if !ok {
			return
		}
		input, ok := decodeJSON[sermons.UpdateInput](w, r)
		if !ok {
			return
		}
		result, err := service.Update(r.Context(), actor, r.PathValue("sermonId"), input)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": result})
	})
}
